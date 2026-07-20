use anyhow::{Context, Result, bail};
use std::io::{self, Write};

use crate::api::ApiClient;
use crate::cli::{AgentArgs, AgentResumeArgs, AgentSessionCreateArgs, AgentTurnCreateArgs};
use crate::interactive::{
    InputPrompt, InteractiveLineReader, PromptLine, prompt_confirm, prompt_input,
};
use serde_json::{Map, Value};

use super::render::stdout::InterruptState;
use super::requests::list_agent_providers;
use super::tui;
use super::types::{AgentInteractionInfo, AgentShell};

pub fn run_interactive(client: &ApiClient, args: &AgentArgs) -> Result<()> {
    let shell = AgentShell::connect(client, args)?;
    run_shell_interactive(client, shell, args.messages.clone())
}

pub fn resume_interactive(client: &ApiClient, args: &AgentResumeArgs) -> Result<()> {
    let shell = AgentShell::resume(client, args)?;
    run_shell_interactive(client, shell, args.messages.clone())
}

impl AgentShell {
    fn connect(client: &ApiClient, args: &AgentArgs) -> Result<Self> {
        let session_args = AgentSessionCreateArgs {
            provider: args.provider.clone(),
            model: args.model.clone(),
            client_ref: None,
            idempotency_key: None,
            tools: args.tools.clone(),
            input: None,
        };
        let session = super::requests::create_session_info(client, &session_args)?;

        Ok(Self {
            session,
            model_override: args.model.clone(),
            system_messages: args.system.clone(),
            tools: args.tools.clone(),
            timeout_seconds: args.timeout_seconds,
            applied_system_messages: false,
        })
    }

    fn resume(client: &ApiClient, args: &AgentResumeArgs) -> Result<Self> {
        let session = match args.session_id.as_deref() {
            Some(session_id) => super::requests::get_session_info(client, session_id)?,
            None => super::requests::resume_latest_session_info(client, args.provider.as_deref())?,
        };

        Ok(Self {
            session,
            model_override: args.model.clone(),
            system_messages: args.system.clone(),
            tools: args.tools.clone(),
            timeout_seconds: args.timeout_seconds,
            applied_system_messages: false,
        })
    }

    fn print_banner(&self) -> Result<()> {
        let mut stderr = io::stderr().lock();
        writeln!(
            stderr,
            "Session {} [{} / {}]",
            self.session.id,
            self.session.provider,
            self.effective_model_label()
        )?;
        writeln!(stderr, "Press Ctrl-C or send EOF to exit.")?;
        Ok(())
    }

    pub(crate) fn effective_model_label(&self) -> &str {
        if let Some(model) = self.model_override.as_deref()
            && !model.trim().is_empty()
        {
            return model;
        }
        if self.session.model.trim().is_empty() {
            "<unspecified>"
        } else {
            self.session.model.as_str()
        }
    }

    fn set_model_override(&mut self, model: &str) {
        self.model_override = Some(model.trim().to_string());
    }

    fn submit_turn(
        &mut self,
        client: &ApiClient,
        messages: Vec<String>,
        interrupts: &super::render::stdout::InterruptState,
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
            timeout_seconds: self.timeout_seconds,
            input: None,
        };
        let turn = super::requests::create_turn_info(client, &turn_args)?;
        self.applied_system_messages = true;
        super::render::stdout::drive_turn(client, &turn, interrupts)?;
        Ok(())
    }
}

pub(crate) fn run_shell_interactive(
    client: &ApiClient,
    mut shell: AgentShell,
    initial_messages: Vec<String>,
) -> Result<()> {
    let session_id = shell.session.id.clone();
    let timeout_seconds = shell.timeout_seconds;
    if tui::can_run() {
        let result = tui::run_shell(client, shell, initial_messages);
        if result.is_ok() {
            print_resume_command(&session_id, timeout_seconds)?;
        }
        return result;
    }

    shell.print_banner()?;
    let interrupts = InterruptState::install();
    let mut input = InteractiveLineReader::with_history_namespace("agent")?;

    if !initial_messages.is_empty() {
        shell.submit_turn(client, initial_messages, &interrupts)?;
    }

    loop {
        let Some(line) = prompt_agent_message(&mut input)? else {
            print_resume_command(&session_id, timeout_seconds)?;
            return Ok(());
        };
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }
        if handle_agent_slash_command(client, &mut shell, trimmed)? {
            continue;
        }
        shell.submit_turn(client, vec![line], &interrupts)?;
    }
}

fn print_resume_command(session_id: &str, timeout_seconds: Option<i32>) -> Result<()> {
    let mut stdout = io::stdout().lock();
    match timeout_seconds {
        Some(timeout_seconds) => writeln!(
            stdout,
            "Resume with: gestalt agent resume {session_id} --timeout-seconds {timeout_seconds}"
        )?,
        None => writeln!(stdout, "Resume with: gestalt agent resume {session_id}")?,
    }
    Ok(())
}

fn handle_agent_slash_command(
    client: &ApiClient,
    shell: &mut AgentShell,
    trimmed: &str,
) -> Result<bool> {
    let Some((command, args)) = parse_agent_slash_command(trimmed) else {
        return Ok(false);
    };
    match command {
        "help" => {
            for line in agent_help_lines() {
                eprintln!("{line}");
            }
        }
        "session" => {
            for line in agent_session_lines(shell) {
                eprintln!("{line}");
            }
        }
        "model" => {
            for line in agent_model_lines(client, shell, args) {
                eprintln!("{line}");
            }
        }
        _ => return Ok(false),
    }
    Ok(true)
}

fn parse_agent_slash_command(input: &str) -> Option<(&str, &str)> {
    let trimmed = input.trim();
    let command = trimmed.strip_prefix('/')?;
    let mut parts = command.splitn(2, char::is_whitespace);
    let command = parts.next().unwrap_or("");
    let args = parts.next().unwrap_or("");
    Some((command, args.trim()))
}

pub(crate) fn agent_help_lines() -> Vec<String> {
    vec![
        "Commands:".to_string(),
        "  /help     Show commands and keys.".to_string(),
        "  /session  Show the active session id and resume command.".to_string(),
        "  /model    Show current model and configured providers.".to_string(),
        "  /model X  Use model X for future turns in this session.".to_string(),
        "Keys:".to_string(),
        "  Enter sends; busy turns queue the prompt.".to_string(),
        "  Alt-Enter inserts a newline.".to_string(),
        "  Up/Down recalls prompt history.".to_string(),
        "  PgUp/PgDn scrolls the transcript.".to_string(),
        "  Ctrl-C cancels, clears input, or exits.".to_string(),
    ]
}

pub(crate) fn agent_tui_help_lines() -> Vec<String> {
    let mut lines = agent_help_lines();
    for line in &mut lines {
        match line.as_str() {
            "  Alt-Enter inserts a newline." => {
                *line =
                    "  Alt-Enter/Ctrl-J/Shift-Enter inserts a newline where supported.".to_string();
            }
            "  Up/Down recalls prompt history." => {
                *line = "  Up/Down recalls input history.".to_string();
            }
            "  PgUp/PgDn scrolls the transcript." => {
                *line = "  PgUp/PgDn scrolls the transcript by half a screen.".to_string();
            }
            _ => {}
        }
    }
    let insert_at = lines
        .iter()
        .position(|line| line.contains("Ctrl-C cancels"))
        .unwrap_or(lines.len());
    lines.insert(
        insert_at,
        "  Ctrl-D exits when idle and input is empty.".to_string(),
    );
    lines.insert(insert_at, "  Ctrl-L redraws the terminal.".to_string());
    lines.insert(
        insert_at,
        "  Ctrl-Home/Ctrl-End jumps to transcript start/end.".to_string(),
    );
    lines.insert(
        insert_at,
        "  Wheel/trackpad scrolls the transcript.".to_string(),
    );
    lines.insert(
        insert_at,
        "  Ctrl-O toggles compact/full tool details.".to_string(),
    );
    lines
}

pub(crate) fn agent_session_lines(shell: &AgentShell) -> Vec<String> {
    let resume_command = match shell.timeout_seconds {
        Some(timeout_seconds) => format!(
            "resume command: gestalt agent resume {} --timeout-seconds {}",
            shell.session.id, timeout_seconds
        ),
        None => format!("resume command: gestalt agent resume {}", shell.session.id),
    };
    vec![format!("session {}", shell.session.id), resume_command]
}

pub(crate) fn agent_model_lines(
    client: &ApiClient,
    shell: &mut AgentShell,
    args: &str,
) -> Vec<String> {
    let requested = args.trim();
    if !requested.is_empty() {
        shell.set_model_override(requested);
        return vec![format!("model {requested} selected for future turns")];
    }

    let mut lines = vec![
        format!("current provider: {}", shell.session.provider),
        format!("current model: {}", shell.effective_model_label()),
    ];
    match list_agent_providers(client) {
        Ok(providers) if providers.is_empty() => {
            lines.push("configured providers: none".to_string());
        }
        Ok(providers) => {
            lines.push("configured providers:".to_string());
            for provider in providers {
                let suffix = if provider.default { " (default)" } else { "" };
                lines.push(format!("  {}{}", provider.name, suffix));
            }
        }
        Err(err) => {
            lines.push(format!("configured providers unavailable: {err}"));
        }
    }
    lines
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
                return Ok(None);
            }
            PromptLine::Eof => return Ok(None),
        }
    }
}

fn has_trailing_continuation(line: &str) -> bool {
    line.chars().rev().take_while(|ch| *ch == '\\').count() % 2 == 1
}

pub(crate) fn prompt_interaction_resolution(
    interaction: &AgentInteractionInfo,
) -> Result<Map<String, Value>> {
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
