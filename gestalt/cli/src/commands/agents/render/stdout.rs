use anyhow::{Context, Result};
use colored::Colorize;
use serde_json::Value;
use std::io::{self, IsTerminal, Write};
use std::sync::{
    Arc,
    atomic::{AtomicBool, AtomicU64, Ordering},
};
use std::thread;
use std::time::Duration;

use crate::api::ApiClient;
use crate::commands::agents::driver::{
    TurnDriverSink, TurnLoopContext, TurnLoopOutcome, resolve_pending_interactions,
    run_turn_status_loop,
};
use crate::commands::agents::events::{
    ClassifiedTurnEvent, ToolPhase, TurnEventEffect, classify_data_event, classify_turn_event,
};
use crate::commands::agents::fields::{
    compact_json, display_action, display_label, display_ref, display_status, display_text,
    display_tool_error, display_tool_input, display_tool_label, display_tool_output,
    display_value_text, message_label, message_text, pretty_json, rendered_display_text,
};
use crate::commands::agents::requests::{
    INTERRUPT_CANCEL_REASON, cancel_turn_silent, resolve_interaction_info,
};
use crate::commands::agents::shell::prompt_interaction_resolution;
use crate::commands::agents::stream::stream_turn_event_frames;
use crate::commands::agents::types::{
    AgentMessageInfo, AgentTurnDisplayInfo, AgentTurnEventInfo, AgentTurnInfo,
};

pub(crate) fn drive_turn(
    client: &ApiClient,
    turn: &AgentTurnInfo,
    interrupts: &InterruptState,
) -> Result<()> {
    let mut renderer = AgentTurnRenderer::new();
    let _cancel_guard = TurnCancelGuard::spawn(client, &turn.id, interrupts);
    let ctx = TurnLoopContext {
        client,
        turn_id: &turn.id,
    };
    let mut sink = ShellTurnDriver {
        renderer: &mut renderer,
        interrupts,
    };
    run_turn_status_loop(&ctx, &mut sink)
}

struct ShellTurnDriver<'a> {
    renderer: &'a mut AgentTurnRenderer,
    interrupts: &'a InterruptState,
}

impl TurnDriverSink for ShellTurnDriver<'_> {
    fn stream_events(&mut self, ctx: &TurnLoopContext<'_>) -> Result<()> {
        let after_seq = self.renderer.after_seq();
        stream_turn_event_frames(ctx.client, ctx.turn_id, after_seq, |event| {
            self.renderer.render_events(&[event])
        })
    }

    fn on_turn_snapshot(&mut self, turn: &AgentTurnInfo) -> Result<()> {
        self.renderer.finish_turn(turn)
    }

    fn handle_waiting_for_input(
        &mut self,
        ctx: &TurnLoopContext<'_>,
        turn: &AgentTurnInfo,
    ) -> Result<TurnLoopOutcome> {
        resolve_pending_interactions(ctx, turn, |interaction| {
            let prompt_interrupt_count = self.interrupts.count();
            let resolution = match prompt_interaction_resolution(&interaction) {
                Ok(resolution) => resolution,
                Err(_) if self.interrupts.count() > prompt_interrupt_count => {
                    let _ = cancel_turn_silent(ctx.client, ctx.turn_id, INTERRUPT_CANCEL_REASON);
                    return Ok(TurnLoopOutcome::Cancelled);
                }
                Err(err) => return Err(err),
            };
            if self.interrupts.count() > prompt_interrupt_count {
                let _ = cancel_turn_silent(ctx.client, ctx.turn_id, INTERRUPT_CANCEL_REASON);
                return Ok(TurnLoopOutcome::Cancelled);
            }
            resolve_interaction_info(ctx.client, ctx.turn_id, &interaction.id, resolution)?;
            Ok(TurnLoopOutcome::Continue)
        })
    }

    fn poll_cancel(&mut self, _ctx: &TurnLoopContext<'_>) -> Result<bool> {
        Ok(false)
    }
}

#[derive(Clone)]
pub(crate) struct InterruptState {
    count: Arc<AtomicU64>,
}

impl InterruptState {
    pub(crate) fn install() -> Self {
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

pub(crate) struct AgentTurnRenderer {
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

    pub(crate) fn after_seq(&self) -> u64 {
        self.after_seq
    }

    fn render_messages(&mut self, messages: &[AgentMessageInfo]) -> Result<()> {
        self.finish_assistant_line();
        for message in messages {
            let Some(label) = message_label(message) else {
                continue;
            };
            let Some(text) = message_text(message) else {
                continue;
            };
            println!("{} {text}", self.label(&label));
        }
        Ok(())
    }

    pub(crate) fn render_events(&mut self, events: &[AgentTurnEventInfo]) -> Result<()> {
        for event in events {
            if event.seq > 0 {
                self.after_seq = self.after_seq.max(event.seq as u64);
            }
            match classify_turn_event(event) {
                ClassifiedTurnEvent::Display { event, display } => {
                    if !self.render_display_event(event, display)? {
                        if let Some(effect) = classify_data_event(event) {
                            self.render_effect(&effect)?;
                        } else {
                            self.render_generic_event(event)?;
                        }
                    }
                }
                ClassifiedTurnEvent::Data(effect) => self.render_effect(&effect)?,
                ClassifiedTurnEvent::Private => {}
                ClassifiedTurnEvent::Unknown(event) => self.render_generic_event(event)?,
            }
        }
        Ok(())
    }

    fn render_effect(&mut self, effect: &TurnEventEffect) -> Result<()> {
        match effect {
            TurnEventEffect::AssistantDelta(text) => {
                self.start_assistant_line()?;
                print!("{text}");
                io::stdout().flush().context("failed to flush stdout")?;
                self.saw_assistant_output = true;
                self.delta_buffer.push_str(text);
            }
            TurnEventEffect::AssistantCompleted { text } => {
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
            TurnEventEffect::TurnStarted { status } => {
                self.finish_assistant_line();
                match status {
                    Some(status) => println!("{} started ({status})", self.label("turn>")),
                    None => println!("{} started", self.label("turn>")),
                }
            }
            TurnEventEffect::Tool(info) => {
                self.finish_assistant_line();
                match info.phase {
                    ToolPhase::Started => {
                        print!("{} {} started", self.label("tool>"), info.name);
                        if let Some(input) = info.input.as_ref() {
                            print!(" {}", compact_json(input)?);
                        }
                        println!();
                    }
                    ToolPhase::Completed | ToolPhase::Failed => {
                        let phase = match info.phase {
                            ToolPhase::Completed => "completed",
                            ToolPhase::Failed => "failed",
                            ToolPhase::Started => unreachable!(),
                        };
                        match info.status.as_deref() {
                            Some(status) => {
                                print!("{} {} {phase} ({status})", self.label("tool>"), info.name);
                            }
                            None => print!("{} {} {phase}", self.label("tool>"), info.name),
                        }
                        if let Some(error) = info.error.as_deref() {
                            print!(": {error}");
                        } else if let Some(output) = info.output.as_ref() {
                            print!(" {}", compact_json(output)?);
                        }
                        println!();
                    }
                }
            }
            TurnEventEffect::InteractionRequested { id } => {
                self.finish_assistant_line();
                println!("{} requested ({id})", self.label("interaction>"));
            }
            TurnEventEffect::InteractionResolved { id } => {
                self.finish_assistant_line();
                println!("{} resolved ({id})", self.label("interaction>"));
            }
            TurnEventEffect::TurnFailed { error } => {
                self.finish_assistant_line();
                if let Some(message) = error {
                    println!("{} failed: {message}", self.label("turn>"));
                }
            }
            TurnEventEffect::TurnCanceled { reason } => {
                self.finish_assistant_line();
                if let Some(reason) = reason {
                    println!("{} canceled: {reason}", self.label("turn>"));
                }
            }
            TurnEventEffect::TurnCompleted { .. } => {}
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

    fn render_display_event(
        &mut self,
        event: &AgentTurnEventInfo,
        display: &AgentTurnDisplayInfo,
    ) -> Result<bool> {
        match display.kind.trim() {
            "text" => self.render_display_text(display, "assistant>"),
            "reasoning" => self.render_display_text(display, "reasoning>"),
            "tool" => self.render_display_tool(display),
            "interaction" => Ok(self.render_display_interaction(event, display)),
            "status" => Ok(self.render_display_status(display)),
            "error" => Ok(self.render_display_error(display)),
            _ => Ok(false),
        }
    }

    fn render_display_text(&mut self, display: &AgentTurnDisplayInfo, label: &str) -> Result<bool> {
        let raw_text = display_text(display);
        let rendered_text = if label == "assistant>" {
            rendered_display_text(display)
        } else {
            raw_text.map(ToString::to_string)
        };
        if label == "assistant>" {
            match display.phase.trim() {
                "delta" => {
                    if let Some(text) = raw_text {
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
                        let Some(text) = raw_text else {
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
                    } else if let Some(text) = rendered_text.as_deref() {
                        println!("{} {text}", self.label(label));
                        self.saw_assistant_output = true;
                        self.delta_buffer.clear();
                        return Ok(true);
                    }
                }
                _ if rendered_text.is_some() => {
                    self.finish_assistant_line();
                    let text = rendered_text.as_deref().expect("checked is_some");
                    println!("{} {text}", self.label(label));
                    self.saw_assistant_output = true;
                    return Ok(true);
                }
                _ => {}
            }
            return Ok(false);
        }

        self.finish_assistant_line();
        if let Some(text) = rendered_text.as_deref() {
            println!("{} {text}", self.label(label));
            return Ok(true);
        }
        Ok(false)
    }

    fn render_display_tool(&mut self, display: &AgentTurnDisplayInfo) -> Result<bool> {
        self.finish_assistant_line();
        let tool = display_tool_label(display);
        let action = display_action(display);
        match display.phase.trim() {
            "started" => {
                print!(
                    "{} {}",
                    self.label("tool>"),
                    action
                        .map(|action| format!("{action} {tool}"))
                        .unwrap_or_else(|| format!("{tool} started"))
                );
                if let Some(input) = display_tool_input(display) {
                    print!(" {}", compact_json(input)?);
                }
                println!();
            }
            "completed" => {
                print!(
                    "{} {}",
                    self.label("tool>"),
                    action
                        .map(|action| format!("{action} {tool}"))
                        .unwrap_or_else(|| format!("{tool} completed"))
                );
                if let Some(status) = display_status(display) {
                    print!(" ({status})");
                }
                if let Some(error) = display_tool_error(display) {
                    print!(": {}", display_value_text(error)?);
                } else if let Some(output) = display_tool_output(display) {
                    print!(" {}", compact_json(output)?);
                }
                println!();
            }
            "failed" => {
                print!(
                    "{} {}",
                    self.label("tool>"),
                    action
                        .map(|action| format!("{action} {tool}"))
                        .unwrap_or_else(|| format!("{tool} failed"))
                );
                if let Some(status) = display_status(display) {
                    print!(" ({status})");
                }
                if let Some(error) = display_tool_error(display) {
                    print!(": {}", display_value_text(error)?);
                }
                println!();
            }
            "progress" => match (action, display_text(display)) {
                (Some(action), Some(text)) => {
                    println!("{} {action} {tool}: {text}", self.label("tool>"));
                }
                (Some(action), None) => {
                    println!("{} {action} {tool}", self.label("tool>"));
                }
                (None, Some(text)) => {
                    println!("{} {tool} {text}", self.label("tool>"));
                }
                (None, None) => {
                    println!("{} {tool} progress", self.label("tool>"));
                }
            },
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
pub(crate) fn render_turn_transcript(
    turn: &AgentTurnInfo,
    events: &[AgentTurnEventInfo],
) -> Result<()> {
    let mut renderer = AgentTurnRenderer::new();
    renderer.render_messages(&turn.messages)?;
    renderer.render_events(events)?;
    renderer.finish_turn(turn)
}
