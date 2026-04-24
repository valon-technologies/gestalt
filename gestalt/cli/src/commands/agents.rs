use anyhow::{Context, Result, bail};
use serde::{Deserialize, de::DeserializeOwned};
use serde_json::{Map, Value, json};
use std::io::{self, Write};
use std::thread;
use std::time::Duration;

use crate::api::ApiClient;
use crate::cli::{
    AgentArgs, AgentSessionCreateArgs, AgentSessionUpdateArgs, AgentToolArg, AgentTurnCreateArgs,
    AgentTurnEventListArgs, AgentTurnEventStreamArgs,
};
use crate::interactive::{InputPrompt, prompt_confirm, prompt_input};
use crate::output::{self, Format};
use crate::params;

const SESSIONS_PATH: &str = "/api/v1/agent/sessions";
const TURNS_PATH: &str = "/api/v1/agent/turns";
const DEFAULT_EVENT_PAGE_SIZE: u32 = 100;
const EVENT_POLL_INTERVAL: Duration = Duration::from_millis(250);

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

pub fn list_turn_events(
    client: &ApiClient,
    args: &AgentTurnEventListArgs,
    format: Format,
) -> Result<()> {
    let resp = client
        .get(&turn_events_path(&args.id, false, args.after, args.limit))
        .with_context(|| format!("failed to list events for agent turn {}", args.id))?;
    print_turn_events(&resp, format);
    Ok(())
}

pub fn stream_turn_events(client: &ApiClient, args: &AgentTurnEventStreamArgs) -> Result<()> {
    let mut resp = client
        .get_stream(&turn_events_path(&args.id, true, args.after, args.limit))
        .with_context(|| format!("failed to stream events for agent turn {}", args.id))?;
    let mut stdout = io::stdout().lock();
    io::copy(&mut resp, &mut stdout).context("failed to read agent turn event stream")?;
    Ok(())
}

pub fn run_interactive(client: &ApiClient, args: &AgentArgs) -> Result<()> {
    let mut shell = AgentShell::connect(client, args)?;
    shell.print_banner()?;

    if !args.messages.is_empty() {
        shell.submit_turn(client, args.messages.clone())?;
    }

    loop {
        let Some(line) = prompt_agent_line()? else {
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
        shell.submit_turn(client, vec![trimmed.to_string()])?;
    }
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct AgentSessionInfo {
    id: String,
    provider: String,
    #[serde(default)]
    model: String,
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
    status_message: String,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct AgentTurnEventInfo {
    seq: i64,
    #[serde(rename = "type")]
    event_type: String,
    #[serde(default)]
    data: Map<String, Value>,
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

    fn submit_turn(&mut self, client: &ApiClient, messages: Vec<String>) -> Result<()> {
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
        drive_turn(client, &turn)?;
        Ok(())
    }
}

fn drive_turn(client: &ApiClient, turn: &AgentTurnInfo) -> Result<()> {
    let mut renderer = AgentTurnRenderer::default();
    loop {
        drain_turn_events(client, &turn.id, &mut renderer)?;
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
                    let resolution = prompt_interaction_resolution(&interaction)?;
                    resolve_interaction_info(client, &turn.id, &interaction.id, resolution)?;
                }
            }
            "pending" | "running" => thread::sleep(EVENT_POLL_INTERVAL),
            "succeeded" | "failed" | "canceled" => return Ok(()),
            other => bail!("agent turn {} has unsupported status {}", latest.id, other),
        }
    }
}

#[derive(Default)]
struct AgentTurnRenderer {
    after_seq: u64,
    assistant_line_open: bool,
    saw_assistant_output: bool,
    delta_buffer: String,
}

impl AgentTurnRenderer {
    fn after_seq(&self) -> u64 {
        self.after_seq
    }

    fn render_events(&mut self, events: &[AgentTurnEventInfo]) -> Result<()> {
        for event in events {
            if event.seq > 0 {
                self.after_seq = self.after_seq.max(event.seq as u64);
            }
            match event.event_type.as_str() {
                "agent.message.delta" | "assistant.delta" => {
                    if let Some(text) = string_field(&event.data, "text") {
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
                        println!("assistant> {text}");
                        self.saw_assistant_output = true;
                    }
                    self.delta_buffer.clear();
                }
                "tool.started" => {
                    self.finish_assistant_line();
                    let tool_id =
                        string_field(&event.data, "tool_id").unwrap_or_else(|| "tool".to_string());
                    println!("tool> {tool_id} started");
                }
                "tool.completed" => {
                    self.finish_assistant_line();
                    let tool_id =
                        string_field(&event.data, "tool_id").unwrap_or_else(|| "tool".to_string());
                    match number_field(&event.data, "status") {
                        Some(status) => println!("tool> {tool_id} completed ({status})"),
                        None => println!("tool> {tool_id} completed"),
                    }
                }
                "interaction.requested" => {
                    self.finish_assistant_line();
                    let interaction_id = string_field(&event.data, "interaction_id")
                        .unwrap_or_else(|| "interaction".to_string());
                    println!("interaction> requested ({interaction_id})");
                }
                "interaction.resolved" => {
                    self.finish_assistant_line();
                    let interaction_id = string_field(&event.data, "interaction_id")
                        .unwrap_or_else(|| "interaction".to_string());
                    println!("interaction> resolved ({interaction_id})");
                }
                "turn.failed" => {
                    self.finish_assistant_line();
                    if let Some(message) = string_field(&event.data, "error") {
                        println!("turn> failed: {message}");
                    }
                }
                "turn.canceled" => {
                    self.finish_assistant_line();
                    if let Some(reason) = string_field(&event.data, "reason") {
                        println!("turn> canceled: {reason}");
                    }
                }
                _ => {}
            }
        }
        Ok(())
    }

    fn finish_turn(&mut self, turn: &AgentTurnInfo) -> Result<()> {
        self.finish_assistant_line();
        match turn.status.as_str() {
            "succeeded" if !self.saw_assistant_output && !turn.output_text.is_empty() => {
                println!("assistant> {}", turn.output_text);
                self.saw_assistant_output = true;
            }
            "failed" if !turn.status_message.is_empty() => {
                println!("turn> failed: {}", turn.status_message);
            }
            "canceled" if !turn.status_message.is_empty() => {
                println!("turn> canceled: {}", turn.status_message);
            }
            _ => {}
        }
        self.delta_buffer.clear();
        Ok(())
    }

    fn start_assistant_line(&mut self) -> Result<()> {
        if !self.assistant_line_open {
            print!("assistant> ");
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
}

fn prompt_agent_line() -> Result<Option<String>> {
    let mut stderr = io::stderr().lock();
    write!(stderr, "agent> ")?;
    stderr.flush()?;

    let mut line = String::new();
    let read = io::stdin()
        .read_line(&mut line)
        .context("failed to read agent input")?;
    if read == 0 {
        writeln!(stderr)?;
        return Ok(None);
    }
    Ok(Some(line.trim().to_string()))
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

fn drain_turn_events(
    client: &ApiClient,
    turn_id: &str,
    renderer: &mut AgentTurnRenderer,
) -> Result<()> {
    loop {
        let events = list_turn_events_info(
            client,
            turn_id,
            renderer.after_seq(),
            DEFAULT_EVENT_PAGE_SIZE,
        )?;
        if events.is_empty() {
            return Ok(());
        }
        let event_count = events.len();
        renderer.render_events(&events)?;
        if event_count < DEFAULT_EVENT_PAGE_SIZE as usize {
            return Ok(());
        }
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

fn list_turn_events_info(
    client: &ApiClient,
    turn_id: &str,
    after: u64,
    limit: u32,
) -> Result<Vec<AgentTurnEventInfo>> {
    decode_json(
        client
            .get(&turn_events_path(turn_id, false, Some(after), Some(limit)))
            .with_context(|| format!("failed to list events for agent turn {turn_id}"))?,
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

fn string_field(data: &Map<String, Value>, key: &str) -> Option<String> {
    data.get(key)
        .and_then(Value::as_str)
        .map(ToString::to_string)
}

fn number_field(data: &Map<String, Value>, key: &str) -> Option<i64> {
    data.get(key).and_then(Value::as_i64)
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
        "pluginName": tool.plugin,
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

fn turn_events_path(id: &str, stream: bool, after: Option<u64>, limit: Option<u32>) -> String {
    let mut serializer = url::form_urlencoded::Serializer::new(String::new());
    if let Some(after) = after {
        serializer.append_pair("after", &after.to_string());
    }
    if let Some(limit) = limit {
        serializer.append_pair("limit", &limit.to_string());
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
