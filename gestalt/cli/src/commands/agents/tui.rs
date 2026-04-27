use std::collections::VecDeque;
use std::io::{self, IsTerminal};
use std::sync::mpsc::{self, Receiver, Sender, TryRecvError};
use std::thread;
use std::time::Duration;

use anyhow::{Context, Result};
use ratatui::crossterm::event::{self, Event, KeyCode, KeyEvent, KeyEventKind, KeyModifiers};
use ratatui::prelude::*;
use ratatui::widgets::{Block, Borders, Clear, Paragraph, Wrap};
use ratatui_textarea::TextArea;
use serde_json::{Map, Value};

use crate::api::ApiClient;
use crate::cli::{AgentArgs, AgentTurnCreateArgs};

mod state;
mod worker;

use state::{AgentUiState, TranscriptKind, turn_status_label};
use worker::{TurnWorker, WorkerCommand, WorkerEvent, spawn_turn_worker};

use super::{
    AgentInteractionInfo, AgentShell, INTERRUPT_CANCEL_REASON, cancel_turn_silent, compact_json,
};

const TICK_RATE: Duration = Duration::from_millis(50);

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
        Ok(Self {
            terminal: ratatui::try_init().context("failed to initialize terminal UI")?,
            restored: false,
        })
    }

    fn inner_mut(&mut self) -> &mut ratatui::DefaultTerminal {
        &mut self.terminal
    }

    fn restore(&mut self) -> Result<()> {
        if !self.restored {
            ratatui::try_restore().context("failed to restore terminal UI")?;
            self.restored = true;
        }
        Ok(())
    }
}

impl Drop for TerminalGuard {
    fn drop(&mut self) {
        if !self.restored {
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
        }
    }

    fn run(&mut self, terminal: &mut ratatui::DefaultTerminal) -> Result<()> {
        loop {
            self.drain_worker_events();
            self.start_next_queued_turn();
            terminal.draw(|frame| self.draw(frame))?;

            if self.should_quit {
                return Ok(());
            }

            if event::poll(TICK_RATE).context("failed to poll terminal events")? {
                match event::read().context("failed to read terminal event")? {
                    Event::Key(key) if key.kind == KeyEventKind::Press => self.handle_key(key),
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
        let mut lines = Vec::new();
        self.transcript_visible_height = area.height.saturating_sub(2).max(1) as usize;
        for item in self
            .state
            .visible_transcript(self.transcript_visible_height)
        {
            let style = match item.kind {
                TranscriptKind::User => Style::default().fg(Color::Cyan),
                TranscriptKind::Assistant => Style::default().fg(Color::White),
                TranscriptKind::Tool => Style::default().fg(Color::Yellow),
                TranscriptKind::Interaction => Style::default().fg(Color::Magenta),
                TranscriptKind::System => Style::default().fg(Color::Green),
                TranscriptKind::Error => Style::default().fg(Color::Red),
            };
            let label = Span::styled(
                format!("{} ", item.kind.label()),
                style.add_modifier(Modifier::BOLD),
            );
            lines.push(Line::from(vec![label, Span::raw(item.text.clone())]));
        }
        if lines.is_empty() {
            lines.push(Line::from("Start typing below."));
        }

        let paragraph = Paragraph::new(lines)
            .block(Block::default().borders(Borders::ALL).title(title))
            .wrap(Wrap { trim: false });
        frame.render_widget(paragraph, area);
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
        let title = if self.state.busy {
            format!("Message - queued {}", self.queued_messages.len())
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
            format!(
                "{} | Enter queue/send | Alt-Enter newline | PgUp/PgDn scroll | Ctrl-C cancel",
                self.state.status
            )
        } else {
            format!(
                "{} | Enter send | Alt-Enter newline | PgUp/PgDn scroll | Ctrl-C clear/exit",
                self.state.status
            )
        };
        frame.render_widget(
            Paragraph::new(status).style(Style::default().fg(Color::DarkGray)),
            area,
        );
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
            (KeyCode::PageUp, _) => self.state.scroll_up(self.transcript_visible_height),
            (KeyCode::PageDown, _) => self.state.scroll_down(),
            (KeyCode::Home, modifiers) if modifiers.contains(KeyModifiers::CONTROL) => {
                self.state.scroll_to_top(self.transcript_visible_height);
            }
            (KeyCode::End, modifiers) if modifiers.contains(KeyModifiers::CONTROL) => {
                self.state.scroll_to_bottom();
            }
            (KeyCode::Enter, modifiers) if modifiers.contains(KeyModifiers::ALT) => {
                self.composer.insert_newline();
            }
            (KeyCode::Enter, _) => self.submit_composer(),
            _ => {
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
                self.state.push_system(
                    "Commands: /help, /session, /quit. Enter sends; Alt-Enter inserts a newline.",
                );
            }
            "/session" => {
                self.state
                    .push_system(format!("session {}", self.shell.session.id));
            }
            _ => self.enqueue_or_start(vec![message]),
        }

        self.composer = styled_textarea("Message");
    }

    fn enqueue_or_start(&mut self, messages: Vec<String>) {
        self.state.push_user(messages.join("\n"));
        if self.state.busy {
            self.queued_messages.push_back(messages);
            self.state.status = "queued".to_string();
            return;
        }
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
            WorkerEvent::TurnEvent(event) => self.state.apply_turn_event(event),
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
            self.composer = styled_textarea("Message");
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
}

fn styled_textarea(title: &'static str) -> TextArea<'static> {
    let mut textarea = TextArea::default();
    textarea.set_cursor_line_style(Style::default());
    textarea.set_block(Block::default().borders(Borders::ALL).title(title));
    textarea
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
