use anyhow::{Context, Result, bail};
use colored::Colorize;
use serde::{Deserialize, Deserializer, de::DeserializeOwned};
use serde_json::{Map, Value, json};
use std::io::{self, BufRead, BufReader, IsTerminal, Write};
use std::sync::{
    Arc,
    atomic::{AtomicBool, AtomicU64, Ordering},
};
use std::thread;
use std::time::Duration;
use time::{OffsetDateTime, format_description::well_known::Rfc3339};

use crate::api::ApiClient;
use crate::cli::{
    AgentArgs, AgentSessionCreateArgs, AgentSessionUpdateArgs, AgentToolArg, AgentTurnCreateArgs,
    AgentTurnEventListArgs, AgentTurnEventStreamArgs,
};
use crate::interactive::{
    InputPrompt, InteractiveLineReader, PromptLine, prompt_confirm, prompt_input,
};
use crate::output::{self, Format};
use crate::params;

mod tui;

const SESSIONS_PATH: &str = "/api/v1/agent/sessions";
const TURNS_PATH: &str = "/api/v1/agent/turns";
const DEFAULT_EVENT_PAGE_SIZE: u32 = 100;
const EVENT_POLL_INTERVAL: Duration = Duration::from_millis(250);
const EVENT_STREAM_UNTIL_BLOCKED_OR_TERMINAL: &str = "blocked_or_terminal";
const INTERRUPT_CANCEL_REASON: &str = "operator interrupted";

pub fn create_session(
    client: &ApiClient,
    args: &AgentSessionCreateArgs,
    format: Format,
) -> Result<()> {
    let body = build_session_create_body(args)?;
    let resp = client
        .post(SESSIONS_PATH, &body)
        .context("failed to create agent session")?;
    print_session(&resp, format);
    Ok(())
}

pub fn list_sessions(
    client: &ApiClient,
    provider: Option<&str>,
    state: Option<&str>,
    format: Format,
) -> Result<()> {
    let resp = client
        .get(&sessions_path(provider, state))
        .context("failed to list agent sessions")?;
    print_sessions(&resp, format);
    Ok(())
}

pub fn get_session(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("{SESSIONS_PATH}/{id}"))
        .with_context(|| format!("failed to get agent session {id}"))?;
    print_session(&resp, format);
    Ok(())
}

pub fn update_session(
    client: &ApiClient,
    args: &AgentSessionUpdateArgs,
    format: Format,
) -> Result<()> {
    let body = build_session_update_body(args)?;
    let resp = client
        .patch(&format!("{SESSIONS_PATH}/{}", args.id), &body)
        .with_context(|| format!("failed to update agent session {}", args.id))?;
    print_session(&resp, format);
    Ok(())
}

pub fn create_turn(client: &ApiClient, args: &AgentTurnCreateArgs, format: Format) -> Result<()> {
    let body = build_turn_create_body(args)?;
    let resp = client
        .post(&format!("{SESSIONS_PATH}/{}/turns", args.session_id), &body)
        .with_context(|| format!("failed to create agent turn in session {}", args.session_id))?;
    print_turn(&resp, format);
    Ok(())
}

pub fn list_turns(
    client: &ApiClient,
    session_id: &str,
    status: Option<&str>,
    format: Format,
) -> Result<()> {
    let resp = client
        .get(&session_turns_path(session_id, status))
        .with_context(|| format!("failed to list agent turns for session {session_id}"))?;
    print_turns(&resp, format);
    Ok(())
}

pub fn get_turn(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .get(&format!("{TURNS_PATH}/{id}"))
        .with_context(|| format!("failed to get agent turn {id}"))?;
    print_turn(&resp, format);
    Ok(())
}

pub fn cancel_turn(
    client: &ApiClient,
    id: &str,
    reason: Option<&str>,
    format: Format,
) -> Result<()> {
    let body = match reason {
        Some(reason) => json!({ "reason": reason }),
        None => json!({}),
    };
    let resp = client
        .post(&format!("{TURNS_PATH}/{id}/cancel"), &body)
        .with_context(|| format!("failed to cancel agent turn {id}"))?;
    print_turn(&resp, format);
    Ok(())
}

fn cancel_turn_silent(client: &ApiClient, id: &str, reason: &str) -> Result<AgentTurnInfo> {
    decode_json(
        client
            .post(
                &format!("{TURNS_PATH}/{id}/cancel"),
                &json!({ "reason": reason }),
            )
            .with_context(|| format!("failed to cancel agent turn {id}"))?,
    )
}

pub fn list_turn_events(
    client: &ApiClient,
    args: &AgentTurnEventListArgs,
    format: Format,
) -> Result<()> {
    let resp = client
        .get(&turn_events_path(
            &args.id, false, args.after, args.limit, None,
        ))
        .with_context(|| format!("failed to list events for agent turn {}", args.id))?;
    print_turn_events(&resp, format);
    Ok(())
}

pub fn stream_turn_events(client: &ApiClient, args: &AgentTurnEventStreamArgs) -> Result<()> {
    let mut resp = client
        .get_stream(&turn_events_path(
            &args.id, true, args.after, args.limit, None,
        ))
        .with_context(|| format!("failed to stream events for agent turn {}", args.id))?;
    let mut stdout = io::stdout().lock();
    io::copy(&mut resp, &mut stdout).context("failed to read agent turn event stream")?;
    Ok(())
}

pub fn run_interactive(client: &ApiClient, args: &AgentArgs) -> Result<()> {
    if tui::can_run() {
        return tui::run(client, args);
    }

    let mut shell = AgentShell::connect(client, args)?;
    shell.print_banner()?;
    let interrupts = InterruptState::install();
    let mut input = InteractiveLineReader::with_history_namespace("agent")?;

    if !args.messages.is_empty() {
        shell.submit_turn(client, args.messages.clone(), &interrupts)?;
    }

    loop {
        let Some(line) = prompt_agent_message(&mut input)? else {
            return Ok(());
        };
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }
        match trimmed {
            "/quit" | "/exit" => return Ok(()),
            "/help" => {
                eprintln!("Commands: /help, /session, /quit");
                continue;
            }
            "/session" => {
                eprintln!("session {}", shell.session.id);
                continue;
            }
            _ => {}
        }
        shell.submit_turn(client, vec![line], &interrupts)?;
    }
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct AgentSessionInfo {
    id: String,
    provider: String,
    #[serde(default)]
    model: String,
    #[serde(default)]
    state: String,
    #[serde(default)]
    last_turn_at: String,
    #[serde(default)]
    created_at: String,
    #[serde(default)]
    updated_at: String,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct AgentTurnInfo {
    id: String,
    #[serde(default)]
    status: String,
    #[serde(default)]
    output_text: String,
    #[serde(default)]
    structured_output: Option<Value>,
    #[serde(default)]
    status_message: String,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct AgentTurnEventInfo {
    #[serde(default)]
    id: String,
    #[serde(default)]
    turn_id: String,
    #[serde(default)]
    seq: i64,
    #[serde(rename = "type")]
    event_type: String,
    #[serde(default)]
    source: String,
    #[serde(default)]
    visibility: String,
    #[serde(default)]
    data: Map<String, Value>,
    #[serde(default, deserialize_with = "deserialize_turn_display")]
    display: Option<AgentTurnDisplayInfo>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct AgentTurnDisplayInfo {
    #[serde(default)]
    kind: String,
    #[serde(default)]
    phase: String,
    #[serde(default)]
    text: String,
    #[serde(default)]
    label: String,
    #[serde(default, rename = "ref")]
    display_ref: String,
    #[allow(dead_code)]
    #[serde(default)]
    parent_ref: String,
    #[serde(default)]
    input: Option<Value>,
    #[serde(default)]
    output: Option<Value>,
    #[serde(default)]
    error: Option<Value>,
}

impl AgentTurnDisplayInfo {
    fn from_value(value: Value) -> Option<Self> {
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
        })
    }
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct AgentInteractionInfo {
    id: String,
    #[serde(rename = "type")]
    interaction_type: String,
    #[serde(default)]
    state: String,
    #[serde(default)]
    title: String,
    #[serde(default)]
    prompt: String,
    #[serde(default)]
    request: Map<String, Value>,
}

struct AgentShell {
    session: AgentSessionInfo,
    model_override: Option<String>,
    system_messages: Vec<String>,
    tools: Vec<AgentToolArg>,
    applied_system_messages: bool,
}

impl AgentShell {
    fn connect(client: &ApiClient, args: &AgentArgs) -> Result<Self> {
        let session = match args.session.as_deref() {
            Some(session_id) => get_session_info(client, session_id)?,
            None if args.resume => resume_latest_session_info(client, args.provider.as_deref())?,
            None => {
                let session_args = AgentSessionCreateArgs {
                    provider: args.provider.clone(),
                    model: args.model.clone(),
                    client_ref: None,
                    idempotency_key: None,
                    input: None,
                };
                create_session_info(client, &session_args)?
            }
        };

        Ok(Self {
            session,
            model_override: args.model.clone(),
            system_messages: args.system.clone(),
            tools: args.tools.clone(),
            applied_system_messages: false,
        })
    }

    fn print_banner(&self) -> Result<()> {
        let mut stderr = io::stderr().lock();
        let model = if self.session.model.is_empty() {
            "<unspecified>"
        } else {
            self.session.model.as_str()
        };
        writeln!(
            stderr,
            "Session {} [{} / {}]",
            self.session.id, self.session.provider, model
        )?;
        writeln!(stderr, "Type /quit to exit.")?;
        Ok(())
    }

    fn submit_turn(
        &mut self,
        client: &ApiClient,
        messages: Vec<String>,
        interrupts: &InterruptState,
    ) -> Result<()> {
        let system_messages = if self.applied_system_messages {
            Vec::new()
        } else {
            self.system_messages.clone()
        };
        let turn_args = AgentTurnCreateArgs {
            session_id: self.session.id.clone(),
            model: self.model_override.clone(),
            system: system_messages,
            messages,
            tools: self.tools.clone(),
            idempotency_key: None,
            input: None,
        };
        let turn = create_turn_info(client, &turn_args)?;
        self.applied_system_messages = true;
        drive_turn(client, &turn, interrupts)?;
        Ok(())
    }
}

fn drive_turn(client: &ApiClient, turn: &AgentTurnInfo, interrupts: &InterruptState) -> Result<()> {
    let mut renderer = AgentTurnRenderer::new();
    let _cancel_guard = TurnCancelGuard::spawn(client, &turn.id, interrupts);
    loop {
        stream_turn_events_until_blocked_or_terminal(client, &turn.id, &mut renderer)?;
        let latest = get_turn_info(client, &turn.id)?;
        renderer.finish_turn(&latest)?;

        match latest.status.as_str() {
            "waiting_for_input" => {
                let interactions = list_interactions_info(client, &turn.id)?;
                let pending: Vec<_> = interactions
                    .into_iter()
                    .filter(|interaction| interaction.state == "pending")
                    .collect();
                if pending.is_empty() {
                    bail!(
                        "agent turn {} is waiting for input without a pending interaction",
                        latest.id
                    );
                }
                for interaction in pending {
                    let prompt_interrupt_count = interrupts.count();
                    let resolution = match prompt_interaction_resolution(&interaction) {
                        Ok(resolution) => resolution,
                        Err(_) if interrupts.count() > prompt_interrupt_count => {
                            let _ = cancel_turn_silent(client, &turn.id, INTERRUPT_CANCEL_REASON);
                            return Ok(());
                        }
                        Err(err) => return Err(err),
                    };
                    if interrupts.count() > prompt_interrupt_count {
                        let _ = cancel_turn_silent(client, &turn.id, INTERRUPT_CANCEL_REASON);
                        return Ok(());
                    }
                    resolve_interaction_info(client, &turn.id, &interaction.id, resolution)?;
                }
            }
            "pending" | "running" => thread::sleep(EVENT_POLL_INTERVAL),
            "succeeded" | "failed" | "canceled" => return Ok(()),
            other => bail!("agent turn {} has unsupported status {}", latest.id, other),
        }
    }
}

#[derive(Clone)]
struct InterruptState {
    count: Arc<AtomicU64>,
}

impl InterruptState {
    fn install() -> Self {
        let count = Arc::new(AtomicU64::new(0));
        let handler_count = Arc::clone(&count);
        if let Err(err) = ctrlc::set_handler(move || {
            handler_count.fetch_add(1, Ordering::SeqCst);
        }) {
            eprintln!("warning: failed to install Ctrl-C handler: {err}");
        }
        Self { count }
    }

    fn count(&self) -> u64 {
        self.count.load(Ordering::SeqCst)
    }
}

struct TurnCancelGuard {
    active: Arc<AtomicBool>,
    handle: Option<thread::JoinHandle<()>>,
}

impl TurnCancelGuard {
    fn spawn(client: &ApiClient, turn_id: &str, interrupts: &InterruptState) -> Self {
        let active = Arc::new(AtomicBool::new(true));
        let thread_active = Arc::clone(&active);
        let client = client.clone();
        let turn_id = turn_id.to_string();
        let interrupts = interrupts.clone();
        let baseline = interrupts.count();
        let handle = thread::spawn(move || {
            while thread_active.load(Ordering::SeqCst) {
                if interrupts.count() > baseline {
                    let _ = cancel_turn_silent(&client, &turn_id, INTERRUPT_CANCEL_REASON);
                    return;
                }
                thread::sleep(Duration::from_millis(100));
            }
        });
        Self {
            active,
            handle: Some(handle),
        }
    }
}

impl Drop for TurnCancelGuard {
    fn drop(&mut self) {
        self.active.store(false, Ordering::SeqCst);
        if let Some(handle) = self.handle.take() {
            let _ = handle.join();
        }
    }
}

struct AgentTurnRenderer {
    after_seq: u64,
    assistant_line_open: bool,
    saw_assistant_output: bool,
    saw_structured_output: bool,
    delta_buffer: String,
    use_color: bool,
}

impl AgentTurnRenderer {
    fn new() -> Self {
        Self {
            after_seq: 0,
            assistant_line_open: false,
            saw_assistant_output: false,
            saw_structured_output: false,
            delta_buffer: String::new(),
            use_color: io::stdout().is_terminal(),
        }
    }

    fn after_seq(&self) -> u64 {
        self.after_seq
    }

    fn render_events(&mut self, events: &[AgentTurnEventInfo]) -> Result<()> {
        for event in events {
            if event.seq > 0 {
                self.after_seq = self.after_seq.max(event.seq as u64);
            }
            if self.render_display_event(event)? {
                continue;
            }
            match event.event_type.as_str() {
                "agent.message.delta" | "assistant.delta" => {
                    if let Some(text) = string_any_field(&event.data, &["text", "delta", "content"])
                    {
                        self.start_assistant_line()?;
                        print!("{text}");
                        io::stdout().flush().context("failed to flush stdout")?;
                        self.saw_assistant_output = true;
                        self.delta_buffer.push_str(&text);
                    }
                }
                "assistant.completed" => {
                    let text = string_field(&event.data, "text");
                    if self.assistant_line_open {
                        if let Some(text) = text.as_deref() {
                            if self.delta_buffer.is_empty() {
                                print!("{text}");
                            } else if let Some(suffix) = text.strip_prefix(&self.delta_buffer) {
                                print!("{suffix}");
                            }
                        }
                        println!();
                        self.assistant_line_open = false;
                    } else if let Some(text) = text {
                        println!("{} {text}", self.label("assistant>"));
                        self.saw_assistant_output = true;
                    }
                    self.delta_buffer.clear();
                }
                "turn.started" => {
                    self.finish_assistant_line();
                    match string_any_field(&event.data, &["status", "state"]) {
                        Some(status) => println!("{} started ({status})", self.label("turn>")),
                        None => println!("{} started", self.label("turn>")),
                    }
                }
                "tool.started" => {
                    self.finish_assistant_line();
                    let tool = string_any_field(
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
                    .unwrap_or_else(|| "tool".to_string());
                    print!("{} {tool} started", self.label("tool>"));
                    if let Some(input) =
                        value_any_field(&event.data, &["arguments", "input", "request"])
                    {
                        print!(" {}", compact_json(input)?);
                    }
                    println!();
                }
                "tool.completed" => {
                    self.finish_assistant_line();
                    let tool = string_any_field(
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
                    .unwrap_or_else(|| "tool".to_string());
                    let status =
                        string_any_field(&event.data, &["status", "state"]).or_else(|| {
                            number_any_field(&event.data, &["status", "statusCode"])
                                .map(|status| status.to_string())
                        });
                    match status {
                        Some(status) => {
                            print!("{} {tool} completed ({status})", self.label("tool>"))
                        }
                        None => print!("{} {tool} completed", self.label("tool>")),
                    }
                    if let Some(error) = string_field(&event.data, "error") {
                        print!(": {error}");
                    } else if let Some(output) =
                        value_any_field(&event.data, &["output", "result", "body"])
                    {
                        print!(" {}", compact_json(output)?);
                    }
                    println!();
                }
                "interaction.requested" => {
                    self.finish_assistant_line();
                    let interaction_id =
                        string_any_field(&event.data, &["interaction_id", "interactionId"])
                            .unwrap_or_else(|| "interaction".to_string());
                    println!(
                        "{} requested ({interaction_id})",
                        self.label("interaction>")
                    );
                }
                "interaction.resolved" => {
                    self.finish_assistant_line();
                    let interaction_id =
                        string_any_field(&event.data, &["interaction_id", "interactionId"])
                            .unwrap_or_else(|| "interaction".to_string());
                    println!("{} resolved ({interaction_id})", self.label("interaction>"));
                }
                "turn.failed" => {
                    self.finish_assistant_line();
                    if let Some(message) = string_field(&event.data, "error") {
                        println!("{} failed: {message}", self.label("turn>"));
                    }
                }
                "turn.canceled" => {
                    self.finish_assistant_line();
                    if let Some(reason) = string_field(&event.data, "reason") {
                        println!("{} canceled: {reason}", self.label("turn>"));
                    }
                }
                "turn.completed" => {}
                _ if event.visibility == "private" => {}
                _ => self.render_generic_event(event)?,
            }
        }
        Ok(())
    }

    fn finish_turn(&mut self, turn: &AgentTurnInfo) -> Result<()> {
        self.finish_assistant_line();
        match turn.status.as_str() {
            "succeeded" if !self.saw_assistant_output && !turn.output_text.is_empty() => {
                println!("{} {}", self.label("assistant>"), turn.output_text);
                self.saw_assistant_output = true;
            }
            "failed" if !turn.status_message.is_empty() => {
                println!("{} failed: {}", self.label("turn>"), turn.status_message);
            }
            "canceled" if !turn.status_message.is_empty() => {
                println!("{} canceled: {}", self.label("turn>"), turn.status_message);
            }
            _ => {}
        }
        if !self.saw_structured_output
            && let Some(structured_output) = turn.structured_output.as_ref()
        {
            println!(
                "{} {}",
                self.label("structured>"),
                pretty_json(structured_output)?
            );
            self.saw_structured_output = true;
        }
        self.delta_buffer.clear();
        Ok(())
    }

    fn start_assistant_line(&mut self) -> Result<()> {
        if !self.assistant_line_open {
            print!("{} ", self.label("assistant>"));
            io::stdout().flush().context("failed to flush stdout")?;
            self.assistant_line_open = true;
        }
        Ok(())
    }

    fn finish_assistant_line(&mut self) {
        if self.assistant_line_open {
            println!();
            self.assistant_line_open = false;
        }
    }

    fn render_display_event(&mut self, event: &AgentTurnEventInfo) -> Result<bool> {
        let Some(display) = turn_event_display(event) else {
            return Ok(false);
        };
        match display.kind.trim() {
            "text" => self.render_display_text(display, "assistant>"),
            "reasoning" => self.render_display_text(display, "reasoning>"),
            "tool" => self.render_display_tool(event, display),
            "interaction" => Ok(self.render_display_interaction(event, display)),
            "status" => Ok(self.render_display_status(display)),
            "error" => Ok(self.render_display_error(display)),
            _ => Ok(false),
        }
    }

    fn render_display_text(&mut self, display: &AgentTurnDisplayInfo, label: &str) -> Result<bool> {
        let text = display_text(display);
        if label == "assistant>" {
            match display.phase.trim() {
                "delta" => {
                    if let Some(text) = text {
                        self.start_assistant_line()?;
                        print!("{text}");
                        io::stdout().flush().context("failed to flush stdout")?;
                        self.saw_assistant_output = true;
                        self.delta_buffer.push_str(text);
                        return Ok(true);
                    }
                }
                "completed" => {
                    if self.assistant_line_open {
                        let Some(text) = text else {
                            return Ok(false);
                        };
                        if self.delta_buffer.is_empty() {
                            print!("{text}");
                        } else if let Some(suffix) = text.strip_prefix(&self.delta_buffer) {
                            print!("{suffix}");
                        }
                        println!();
                        self.assistant_line_open = false;
                        self.delta_buffer.clear();
                        return Ok(true);
                    } else if let Some(text) = text {
                        println!("{} {text}", self.label(label));
                        self.saw_assistant_output = true;
                        self.delta_buffer.clear();
                        return Ok(true);
                    }
                }
                _ if text.is_some() => {
                    self.finish_assistant_line();
                    let text = text.expect("checked is_some");
                    println!("{} {text}", self.label(label));
                    self.saw_assistant_output = true;
                    return Ok(true);
                }
                _ => {}
            }
            return Ok(false);
        }

        self.finish_assistant_line();
        if let Some(text) = text {
            println!("{} {text}", self.label(label));
            return Ok(true);
        }
        Ok(false)
    }

    fn render_display_tool(
        &mut self,
        event: &AgentTurnEventInfo,
        display: &AgentTurnDisplayInfo,
    ) -> Result<bool> {
        self.finish_assistant_line();
        let tool = display_tool_label(event, display);
        match display.phase.trim() {
            "started" => {
                print!("{} {tool} started", self.label("tool>"));
                if let Some(input) = display_tool_input(event, display) {
                    print!(" {}", compact_json(input)?);
                }
                println!();
            }
            "completed" => {
                print!("{} {tool} completed", self.label("tool>"));
                if let Some(status) = display_status(event, display) {
                    print!(" ({status})");
                }
                if let Some(error) = display_tool_error(event, display) {
                    print!(": {}", display_value_text(error)?);
                } else if let Some(output) = display_tool_output(event, display) {
                    print!(" {}", compact_json(output)?);
                }
                println!();
            }
            "failed" => {
                print!("{} {tool} failed", self.label("tool>"));
                if let Some(status) = display_status(event, display) {
                    print!(" ({status})");
                }
                if let Some(error) = display_tool_error(event, display) {
                    print!(": {}", display_value_text(error)?);
                }
                println!();
            }
            "progress" => {
                if let Some(text) = display_text(display) {
                    println!("{} {tool} {text}", self.label("tool>"));
                } else {
                    println!("{} {tool} progress", self.label("tool>"));
                }
            }
            _ => return Ok(false),
        }
        Ok(true)
    }

    fn render_display_interaction(
        &mut self,
        _event: &AgentTurnEventInfo,
        display: &AgentTurnDisplayInfo,
    ) -> bool {
        self.finish_assistant_line();
        let interaction_ref = display_ref(display)
            .map(ToString::to_string)
            .unwrap_or_else(|| "interaction".to_string());
        match display.phase.trim() {
            "requested" => println!(
                "{} requested ({interaction_ref})",
                self.label("interaction>")
            ),
            "resolved" => println!(
                "{} resolved ({interaction_ref})",
                self.label("interaction>")
            ),
            _ => return false,
        }
        true
    }

    fn render_display_status(&mut self, display: &AgentTurnDisplayInfo) -> bool {
        self.finish_assistant_line();
        let text = display_text(display);
        match display.phase.trim() {
            "started" => match text {
                Some(text) => println!("{} started ({text})", self.label("turn>")),
                None => println!("{} started", self.label("turn>")),
            },
            "canceled" => match text {
                Some(text) => println!("{} canceled: {text}", self.label("turn>")),
                None => println!("{} canceled", self.label("turn>")),
            },
            "completed" => {
                if let Some(text) = text {
                    println!("{} completed ({text})", self.label("turn>"));
                }
            }
            "progress" => {
                if let Some(text) = text {
                    println!("{} {text}", self.label("turn>"));
                }
            }
            _ => return false,
        }
        true
    }

    fn render_display_error(&mut self, display: &AgentTurnDisplayInfo) -> bool {
        self.finish_assistant_line();
        let label = if display_label(display) == Some("turn") {
            "turn>"
        } else {
            "error>"
        };
        let text = display_text(display).map(ToString::to_string).or_else(|| {
            display
                .error
                .as_ref()
                .and_then(|value| display_value_text(value).ok())
        });
        let Some(text) = text else {
            return false;
        };
        match display.phase.trim() {
            "failed" => println!("{} failed: {text}", self.label(label)),
            _ => println!("{} {text}", self.label(label)),
        }
        true
    }

    fn render_generic_event(&mut self, event: &AgentTurnEventInfo) -> Result<()> {
        self.finish_assistant_line();
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
            println!("{} {}{}", self.label("event>"), event.event_type, suffix);
        } else {
            println!(
                "{} {}{} {}",
                self.label("event>"),
                event.event_type,
                suffix,
                compact_json(&Value::Object(event.data.clone()))?
            );
        }
        Ok(())
    }

    fn label(&self, value: &str) -> String {
        if self.use_color {
            value.bold().cyan().to_string()
        } else {
            value.to_string()
        }
    }
}

fn prompt_agent_message(input: &mut InteractiveLineReader) -> Result<Option<String>> {
    let mut lines = Vec::new();
    let mut prompt = "agent> ";
    loop {
        match input.read_line(prompt)? {
            PromptLine::Line(mut line) => {
                let continued = has_trailing_continuation(&line);
                if continued {
                    line.pop();
                }
                lines.push(line);
                if !continued {
                    return Ok(Some(lines.join("\n")));
                }
                prompt = "...> ";
            }
            PromptLine::Interrupted => {
                eprintln!("^C");
                return Ok(Some(String::new()));
            }
            PromptLine::Eof => return Ok(None),
        }
    }
}

fn has_trailing_continuation(line: &str) -> bool {
    line.chars().rev().take_while(|ch| *ch == '\\').count() % 2 == 1
}

fn prompt_interaction_resolution(interaction: &AgentInteractionInfo) -> Result<Map<String, Value>> {
    let mut stderr = io::stderr().lock();
    writeln!(stderr)?;
    writeln!(
        stderr,
        "Interaction {} [{}]",
        interaction.id, interaction.interaction_type
    )?;
    if !interaction.title.is_empty() {
        writeln!(stderr, "{}", interaction.title)?;
    }
    if !interaction.prompt.is_empty() {
        writeln!(stderr, "{}", interaction.prompt)?;
    }
    if !interaction.request.is_empty() {
        writeln!(
            stderr,
            "Request: {}",
            serde_json::to_string(&interaction.request)
                .context("failed to encode interaction request")?
        )?;
    }
    drop(stderr);

    match interaction.interaction_type.as_str() {
        "approval" => {
            let approved = prompt_confirm("Approve?", true)?;
            Ok(Map::from_iter([(
                "approved".to_string(),
                Value::Bool(approved),
            )]))
        }
        "clarification" | "input" => {
            let default = interaction
                .request
                .get("default")
                .and_then(Value::as_str)
                .map(ToString::to_string);
            let required = interaction
                .request
                .get("required")
                .and_then(Value::as_bool)
                .unwrap_or(true);
            let secret = interaction
                .request
                .get("secret")
                .and_then(Value::as_bool)
                .unwrap_or(false);
            let label = if interaction.title.is_empty() {
                "Response".to_string()
            } else {
                interaction.title.clone()
            };
            let description = if interaction.prompt.is_empty() {
                None
            } else {
                Some(interaction.prompt.clone())
            };
            let response = prompt_input(&InputPrompt {
                label,
                description,
                default,
                required,
                secret,
            })?;
            Ok(Map::from_iter([(
                "response".to_string(),
                Value::String(response),
            )]))
        }
        other => bail!("unsupported agent interaction type {other}"),
    }
}

fn stream_turn_events_until_blocked_or_terminal(
    client: &ApiClient,
    turn_id: &str,
    renderer: &mut AgentTurnRenderer,
) -> Result<()> {
    let after_seq = renderer.after_seq();
    stream_turn_event_frames(client, turn_id, after_seq, |event| {
        renderer.render_events(&[event])
    })
}

fn stream_turn_event_frames<F>(
    client: &ApiClient,
    turn_id: &str,
    after_seq: u64,
    mut handle_event: F,
) -> Result<()>
where
    F: FnMut(AgentTurnEventInfo) -> Result<()>,
{
    let resp = client
        .get_stream(&turn_events_path(
            turn_id,
            true,
            Some(after_seq),
            Some(DEFAULT_EVENT_PAGE_SIZE),
            Some(EVENT_STREAM_UNTIL_BLOCKED_OR_TERMINAL),
        ))
        .with_context(|| format!("failed to stream events for agent turn {turn_id}"))?;
    let mut reader = BufReader::new(resp);
    let mut line = String::new();
    let mut decoder = SseEventDecoder::default();

    loop {
        line.clear();
        let read = reader
            .read_line(&mut line)
            .context("failed to read agent turn event stream")?;
        if read == 0 {
            if let Some(event) = decoder.finish()? {
                handle_event(event)?;
            }
            return Ok(());
        }

        if let Some(event) = decoder.push_line(&line)? {
            handle_event(event)?;
        }
    }
}

#[derive(Default)]
struct SseEventDecoder {
    data: String,
}

impl SseEventDecoder {
    fn push_line(&mut self, line: &str) -> Result<Option<AgentTurnEventInfo>> {
        let trimmed = line.trim_end_matches(['\r', '\n']);
        if trimmed.is_empty() {
            return self.finish();
        }

        if let Some(value) = trimmed.strip_prefix("data:") {
            if !self.data.is_empty() {
                self.data.push('\n');
            }
            self.data.push_str(value.strip_prefix(' ').unwrap_or(value));
        }

        Ok(None)
    }

    fn finish(&mut self) -> Result<Option<AgentTurnEventInfo>> {
        if self.data.is_empty() {
            return Ok(None);
        }
        let raw = std::mem::take(&mut self.data);
        serde_json::from_str(&raw)
            .with_context(|| format!("failed to decode agent turn event stream frame: {raw}"))
            .map(Some)
    }
}

fn create_session_info(
    client: &ApiClient,
    args: &AgentSessionCreateArgs,
) -> Result<AgentSessionInfo> {
    let body = build_session_create_body(args)?;
    decode_json(
        client
            .post(SESSIONS_PATH, &body)
            .context("failed to create agent session")?,
    )
}

fn get_session_info(client: &ApiClient, id: &str) -> Result<AgentSessionInfo> {
    decode_json(
        client
            .get(&format!("{SESSIONS_PATH}/{id}"))
            .with_context(|| format!("failed to get agent session {id}"))?,
    )
}

fn resume_latest_session_info(
    client: &ApiClient,
    provider: Option<&str>,
) -> Result<AgentSessionInfo> {
    let sessions: Vec<AgentSessionInfo> = decode_json(
        client
            .get(&sessions_path(provider, Some("active")))
            .context("failed to list active agent sessions")?,
    )?;
    sessions
        .into_iter()
        .filter(|session| session.state.is_empty() || session.state == "active")
        .max_by(compare_sessions_for_resume)
        .ok_or_else(|| match provider {
            Some(provider) => anyhow::anyhow!(
                "no active agent sessions found for provider {provider}; omit --resume to create one"
            ),
            None => anyhow::anyhow!(
                "no active agent sessions found; omit --resume to create one"
            ),
        })
}

fn compare_sessions_for_resume(a: &AgentSessionInfo, b: &AgentSessionInfo) -> std::cmp::Ordering {
    compare_session_time_field(&a.last_turn_at, &b.last_turn_at)
        .then_with(|| compare_session_time_field(&a.updated_at, &b.updated_at))
        .then_with(|| compare_session_time_field(&a.created_at, &b.created_at))
        .then_with(|| a.id.cmp(&b.id))
}

fn compare_session_time_field(a: &str, b: &str) -> std::cmp::Ordering {
    match (parse_session_time(a), parse_session_time(b)) {
        (Some(a), Some(b)) => a.cmp(&b),
        (Some(_), None) => std::cmp::Ordering::Greater,
        (None, Some(_)) => std::cmp::Ordering::Less,
        (None, None) => a.cmp(b),
    }
}

fn parse_session_time(value: &str) -> Option<OffsetDateTime> {
    OffsetDateTime::parse(value, &Rfc3339).ok()
}

fn create_turn_info(client: &ApiClient, args: &AgentTurnCreateArgs) -> Result<AgentTurnInfo> {
    let body = build_turn_create_body(args)?;
    decode_json(
        client
            .post(&format!("{SESSIONS_PATH}/{}/turns", args.session_id), &body)
            .with_context(|| {
                format!("failed to create agent turn in session {}", args.session_id)
            })?,
    )
}

fn get_turn_info(client: &ApiClient, id: &str) -> Result<AgentTurnInfo> {
    decode_json(
        client
            .get(&format!("{TURNS_PATH}/{id}"))
            .with_context(|| format!("failed to get agent turn {id}"))?,
    )
}

fn list_interactions_info(client: &ApiClient, turn_id: &str) -> Result<Vec<AgentInteractionInfo>> {
    decode_json(
        client
            .get(&format!("{TURNS_PATH}/{turn_id}/interactions"))
            .with_context(|| format!("failed to list interactions for agent turn {turn_id}"))?,
    )
}

fn resolve_interaction_info(
    client: &ApiClient,
    turn_id: &str,
    interaction_id: &str,
    resolution: Map<String, Value>,
) -> Result<AgentInteractionInfo> {
    decode_json(
        client
            .post(
                &format!("{TURNS_PATH}/{turn_id}/interactions/{interaction_id}/resolve"),
                &json!({ "resolution": resolution }),
            )
            .with_context(|| format!("failed to resolve interaction {interaction_id}"))?,
    )
}

fn decode_json<T>(value: Value) -> Result<T>
where
    T: DeserializeOwned,
{
    serde_json::from_value(value).context("failed to decode agent response")
}

fn deserialize_turn_display<'de, D>(
    deserializer: D,
) -> std::result::Result<Option<AgentTurnDisplayInfo>, D::Error>
where
    D: Deserializer<'de>,
{
    let value = Option::<Value>::deserialize(deserializer)?;
    Ok(value.and_then(AgentTurnDisplayInfo::from_value))
}

fn take_string_field(data: &mut Map<String, Value>, key: &str) -> String {
    match data.remove(key) {
        Some(Value::String(value)) => value,
        _ => String::new(),
    }
}

fn string_field(data: &Map<String, Value>, key: &str) -> Option<String> {
    data.get(key)
        .and_then(Value::as_str)
        .map(ToString::to_string)
}

fn string_any_field(data: &Map<String, Value>, keys: &[&str]) -> Option<String> {
    keys.iter().find_map(|key| string_field(data, key))
}

fn value_any_field<'a>(data: &'a Map<String, Value>, keys: &[&str]) -> Option<&'a Value> {
    keys.iter().find_map(|key| data.get(*key))
}

fn number_any_field(data: &Map<String, Value>, keys: &[&str]) -> Option<i64> {
    keys.iter().find_map(|key| data.get(*key)?.as_i64())
}

fn turn_event_display(event: &AgentTurnEventInfo) -> Option<&AgentTurnDisplayInfo> {
    let display = event.display.as_ref()?;
    if display.kind.trim().is_empty() {
        return None;
    }
    if event.visibility == "private" && !known_turn_event_type(&event.event_type) {
        return None;
    }
    Some(display)
}

fn known_turn_event_type(event_type: &str) -> bool {
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

fn display_text(display: &AgentTurnDisplayInfo) -> Option<&str> {
    if display.text.is_empty() {
        None
    } else {
        Some(&display.text)
    }
}

fn display_label(display: &AgentTurnDisplayInfo) -> Option<&str> {
    non_empty_str(&display.label)
}

fn display_ref(display: &AgentTurnDisplayInfo) -> Option<&str> {
    non_empty_str(&display.display_ref)
}

fn display_tool_label(_event: &AgentTurnEventInfo, display: &AgentTurnDisplayInfo) -> String {
    display_label(display)
        .or_else(|| display_ref(display))
        .map(ToString::to_string)
        .unwrap_or_else(|| "tool".to_string())
}

fn display_tool_ref(_event: &AgentTurnEventInfo, display: &AgentTurnDisplayInfo) -> Option<String> {
    display_ref(display).map(ToString::to_string)
}

fn display_tool_input<'a>(
    _event: &'a AgentTurnEventInfo,
    display: &'a AgentTurnDisplayInfo,
) -> Option<&'a Value> {
    display.input.as_ref()
}

fn display_tool_output<'a>(
    _event: &'a AgentTurnEventInfo,
    display: &'a AgentTurnDisplayInfo,
) -> Option<&'a Value> {
    display.output.as_ref()
}

fn display_tool_error<'a>(
    _event: &'a AgentTurnEventInfo,
    display: &'a AgentTurnDisplayInfo,
) -> Option<&'a Value> {
    display.error.as_ref()
}

fn display_status(_event: &AgentTurnEventInfo, display: &AgentTurnDisplayInfo) -> Option<String> {
    display_text(display).map(ToString::to_string)
}

fn display_value_text(value: &Value) -> Result<String> {
    match value {
        Value::String(text) => Ok(text.clone()),
        _ => compact_json(value),
    }
}

fn non_empty_str(value: &str) -> Option<&str> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        None
    } else {
        Some(trimmed)
    }
}

fn compact_json(value: &Value) -> Result<String> {
    serde_json::to_string(value).context("failed to encode compact JSON")
}

fn pretty_json(value: &Value) -> Result<String> {
    serde_json::to_string_pretty(value).context("failed to encode pretty JSON")
}

fn build_session_create_body(args: &AgentSessionCreateArgs) -> Result<Value> {
    let mut body = match args.input.as_deref() {
        Some(path) => params::load_input_file(path)?,
        None => Map::new(),
    };

    if let Some(provider) = args.provider.as_deref() {
        body.insert("provider".to_string(), Value::String(provider.to_string()));
    }
    if let Some(model) = args.model.as_deref() {
        body.insert("model".to_string(), Value::String(model.to_string()));
    }
    if let Some(client_ref) = args.client_ref.as_deref() {
        body.insert(
            "clientRef".to_string(),
            Value::String(client_ref.to_string()),
        );
    }
    if let Some(idempotency_key) = args.idempotency_key.as_deref() {
        body.insert(
            "idempotencyKey".to_string(),
            Value::String(idempotency_key.to_string()),
        );
    }

    Ok(Value::Object(body))
}

fn build_session_update_body(args: &AgentSessionUpdateArgs) -> Result<Value> {
    let mut body = match args.input.as_deref() {
        Some(path) => params::load_input_file(path)?,
        None => Map::new(),
    };

    body.remove("provider");
    body.remove("model");

    if let Some(client_ref) = args.client_ref.as_deref() {
        body.insert(
            "clientRef".to_string(),
            Value::String(client_ref.to_string()),
        );
    }
    if let Some(state) = args.state.as_deref() {
        body.insert("state".to_string(), Value::String(state.to_string()));
    }

    Ok(Value::Object(body))
}

fn build_turn_create_body(args: &AgentTurnCreateArgs) -> Result<Value> {
    let mut body = match args.input.as_deref() {
        Some(path) => params::load_input_file(path)?,
        None => Map::new(),
    };

    body.remove("provider");
    body.remove("clientRef");
    body.remove("state");

    if let Some(model) = args.model.as_deref() {
        body.insert("model".to_string(), Value::String(model.to_string()));
    }
    if let Some(idempotency_key) = args.idempotency_key.as_deref() {
        body.insert(
            "idempotencyKey".to_string(),
            Value::String(idempotency_key.to_string()),
        );
    }

    let messages = build_messages(&args.system, &args.messages);
    if !messages.is_empty() {
        body.insert("messages".to_string(), Value::Array(messages));
    }
    if !args.tools.is_empty() {
        body.insert(
            "toolRefs".to_string(),
            Value::Array(args.tools.iter().map(agent_tool_ref_value).collect()),
        );
    }

    validate_turn_create_body(&body)?;
    Ok(Value::Object(body))
}

fn validate_turn_create_body(body: &Map<String, Value>) -> Result<()> {
    let has_messages = body
        .get("messages")
        .and_then(Value::as_array)
        .is_some_and(|messages| !messages.is_empty());
    if !has_messages {
        bail!(
            "agent turns create requires at least one message; pass --message, --system, or --input with a non-empty messages array"
        );
    }
    Ok(())
}

fn build_messages(system: &[String], messages: &[String]) -> Vec<Value> {
    let mut out = Vec::with_capacity(system.len() + messages.len());
    for text in system {
        out.push(json!({ "role": "system", "text": text }));
    }
    for text in messages {
        out.push(json!({ "role": "user", "text": text }));
    }
    out
}

fn agent_tool_ref_value(tool: &AgentToolArg) -> Value {
    json!({
        "plugin": tool.plugin,
        "operation": tool.operation,
    })
}

fn sessions_path(provider: Option<&str>, state: Option<&str>) -> String {
    let mut serializer = url::form_urlencoded::Serializer::new(String::new());
    if let Some(provider) = provider {
        serializer.append_pair("provider", provider);
    }
    if let Some(state) = state {
        serializer.append_pair("state", state);
    }
    let query = serializer.finish();
    if query.is_empty() {
        SESSIONS_PATH.to_string()
    } else {
        format!("{SESSIONS_PATH}?{query}")
    }
}

fn session_turns_path(session_id: &str, status: Option<&str>) -> String {
    let mut serializer = url::form_urlencoded::Serializer::new(String::new());
    if let Some(status) = status {
        serializer.append_pair("status", status);
    }
    let query = serializer.finish();
    let path = format!("{SESSIONS_PATH}/{session_id}/turns");
    if query.is_empty() {
        path
    } else {
        format!("{path}?{query}")
    }
}

fn turn_events_path(
    id: &str,
    stream: bool,
    after: Option<u64>,
    limit: Option<u32>,
    until: Option<&str>,
) -> String {
    let mut serializer = url::form_urlencoded::Serializer::new(String::new());
    if let Some(after) = after {
        serializer.append_pair("after", &after.to_string());
    }
    if let Some(limit) = limit {
        serializer.append_pair("limit", &limit.to_string());
    }
    if let Some(until) = until {
        serializer.append_pair("until", until);
    }
    let suffix = if stream { "/events/stream" } else { "/events" };
    let query = serializer.finish();
    let path = format!("{TURNS_PATH}/{id}{suffix}");
    if query.is_empty() {
        path
    } else {
        format!("{path}?{query}")
    }
}

fn print_session(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let rows = vec![session_row(value)];
            output::print_table(&session_headers(), &rows);
        }
    }
}

fn print_sessions(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().cloned().unwrap_or_default();
            let rows: Vec<Vec<String>> = items.iter().map(session_row).collect();
            output::print_table(&session_headers(), &rows);
        }
    }
}

fn print_turn(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let rows = vec![turn_row(value)];
            output::print_table(&turn_headers(), &rows);
        }
    }
}

fn print_turns(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().cloned().unwrap_or_default();
            let rows: Vec<Vec<String>> = items.iter().map(turn_row).collect();
            output::print_table(&turn_headers(), &rows);
        }
    }
}

fn print_turn_events(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => {
            let items = value.as_array().cloned().unwrap_or_default();
            let rows: Vec<Vec<String>> = items.iter().map(turn_event_row).collect();
            output::print_table(&event_headers(), &rows);
        }
    }
}

fn session_headers() -> [&'static str; 6] {
    ["ID", "Provider", "Model", "State", "Client Ref", "Updated"]
}

fn turn_headers() -> [&'static str; 6] {
    ["ID", "Session", "Provider", "Model", "Status", "Created"]
}

fn event_headers() -> [&'static str; 7] {
    [
        "Seq",
        "Type",
        "Source",
        "Visibility",
        "Turn",
        "Created",
        "Data",
    ]
}

fn session_row(value: &Value) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        value["provider"].as_str().unwrap_or("-").to_string(),
        value["model"].as_str().unwrap_or("-").to_string(),
        value["state"].as_str().unwrap_or("-").to_string(),
        value["clientRef"].as_str().unwrap_or("-").to_string(),
        value["updatedAt"].as_str().unwrap_or("-").to_string(),
    ]
}

fn turn_row(value: &Value) -> Vec<String> {
    vec![
        value["id"].as_str().unwrap_or("-").to_string(),
        value["sessionId"].as_str().unwrap_or("-").to_string(),
        value["provider"].as_str().unwrap_or("-").to_string(),
        value["model"].as_str().unwrap_or("-").to_string(),
        value["status"].as_str().unwrap_or("-").to_string(),
        value["createdAt"].as_str().unwrap_or("-").to_string(),
    ]
}

fn turn_event_row(value: &Value) -> Vec<String> {
    vec![
        value["seq"]
            .as_i64()
            .map(|seq| seq.to_string())
            .unwrap_or_else(|| "-".to_string()),
        value["type"].as_str().unwrap_or("-").to_string(),
        value["source"].as_str().unwrap_or("-").to_string(),
        value["visibility"].as_str().unwrap_or("-").to_string(),
        value["turnId"].as_str().unwrap_or("-").to_string(),
        value["createdAt"].as_str().unwrap_or("-").to_string(),
        serde_json::to_string(&value["data"]).unwrap_or_else(|_| "-".to_string()),
    ]
}
