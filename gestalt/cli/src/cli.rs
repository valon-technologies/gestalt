use clap::{Args, Parser, Subcommand};

use crate::output::Format;
use crate::params;

#[derive(Parser)]
#[command(name = "gestalt")]
#[command(about = "CLI for Gestalt API - authentication, app, workflow, agent, and operations")]
#[command(version)]
pub struct Cli {
    #[command(subcommand)]
    pub command: Option<Commands>,

    /// Output format
    #[arg(long, global = true, value_enum, default_value_t = Format::Table)]
    pub format: Format,

    /// API server URL (overrides config and env)
    #[arg(long, global = true)]
    pub url: Option<String>,
}

#[derive(Subcommand)]
pub enum Commands {
    /// Run an interactive agent session or inspect agent resources
    Agent(AgentArgs),

    /// Manage apps
    #[command(alias = "apps")]
    App {
        #[command(subcommand)]
        command: AppCommands,
    },

    /// Manage authentication (login, logout)
    Auth {
        #[command(subcommand)]
        command: AuthCommands,
    },

    /// Manage persistent configuration
    Config {
        #[command(subcommand)]
        command: ConfigCommands,
    },

    #[command(hide = true)]
    /// Describe an app operation
    Describe(DescribeArgs),

    /// Interactive setup wizard
    Init,

    #[command(hide = true)]
    /// Execute an app operation
    Invoke(InvokeArgs),

    /// Manage workflow resources
    #[command(alias = "workflows")]
    Workflow {
        #[command(subcommand)]
        command: WorkflowCommands,
    },
}

#[derive(Subcommand)]
pub enum AuthCommands {
    /// Log in via browser OAuth flow
    Login,
    /// Log out and clear stored credentials
    Logout,
    /// Show authentication status
    Status,
    /// Manage API tokens
    Token {
        #[command(subcommand)]
        command: TokenCommands,
    },
}

#[derive(Subcommand)]
pub enum ConfigCommands {
    /// Get a config value
    Get {
        /// Config key
        key: String,
    },
    /// List all config values
    List,
    /// Set a config value
    Set {
        /// Config key
        key: String,
        /// Config value
        value: String,
    },
    /// Remove a config value
    Unset {
        /// Config key
        key: String,
    },
}

#[derive(Subcommand)]
pub enum AppCommands {
    /// Connect an app via OAuth or interactive manual auth
    Connect {
        /// App name (e.g., github, slack)
        name: String,

        /// Named connection to connect
        #[arg(long)]
        connection: Option<String>,

        /// Instance name to create or refresh
        #[arg(long)]
        instance: Option<String>,
    },
    /// Describe an app operation
    Describe(DescribeArgs),
    /// Disconnect an app
    Disconnect {
        /// App name (e.g., github, slack)
        name: String,

        /// Target a specific named connection
        #[arg(long)]
        connection: Option<String>,

        /// Target a specific stored instance
        #[arg(long)]
        instance: Option<String>,
    },
    /// Execute an app operation
    Invoke(InvokeArgs),
    /// List available apps
    List,
}

#[derive(Args)]
pub struct InvokeArgs {
    /// App name (e.g., github, slack)
    pub app: String,

    /// Operation name segments joined by "." (e.g., "chat postMessage" or "chat.postMessage"). Omit to list available operations.
    pub operation: Vec<String>,

    /// Parameters as key=value or key:=json pairs
    #[arg(short = 'p', long = "param", value_parser = params::parse_param_entry)]
    pub params: Vec<params::ParamEntry>,

    /// Select a named connection for this invocation
    #[arg(long)]
    pub connection: Option<String>,

    /// Select a stored connection instance
    #[arg(long)]
    pub instance: Option<String>,

    /// Select a sub-path from the response (e.g., "data.items")
    #[arg(long = "select")]
    pub select: Option<String>,

    /// Load parameters from a JSON file (use "-" for stdin)
    #[arg(long = "input-file")]
    pub input_file: Option<String>,
}

#[derive(Args)]
pub struct DescribeArgs {
    /// App name
    pub app: String,
    /// Operation name
    pub operation: String,
    /// Select a named connection for this operation catalog
    #[arg(long)]
    pub connection: Option<String>,
    /// Select a stored connection instance for this operation catalog
    #[arg(long)]
    pub instance: Option<String>,
}

#[derive(Subcommand)]
pub enum TokenCommands {
    /// Create a new API token
    Create {
        /// Display name for the token
        #[arg(long)]
        name: Option<String>,
    },
    /// List all API tokens
    List,
    /// Revoke an API token
    Revoke {
        /// Token ID to revoke
        id: String,
    },
}
#[derive(Subcommand)]
pub enum WorkflowCommands {
    /// Deliver workflow events
    Events {
        #[command(subcommand)]
        command: WorkflowEventCommands,
    },
    /// Inspect workflow runs
    Runs {
        #[command(subcommand)]
        command: WorkflowRunCommands,
    },
}

#[derive(Args)]
pub struct AgentArgs {
    #[command(subcommand)]
    pub command: Option<AgentCommands>,

    /// Run the agent harness locally (default for `gestalt agent`)
    #[arg(long, conflicts_with = "cloud")]
    pub local: bool,

    /// Run through the configured Gestalt server
    #[arg(long, conflicts_with = "local")]
    pub cloud: bool,

    /// Agent harness name for local launch; defaults to the server-selected harness
    #[arg(long)]
    pub harness: Option<String>,

    /// Agent provider name for a new session
    #[arg(long)]
    pub provider: Option<String>,

    /// Model name override
    #[arg(long)]
    pub model: Option<String>,

    /// Add a system message to the first turn created in this CLI session
    #[arg(long = "system")]
    pub system: Vec<String>,

    /// Start with one or more user messages before entering the prompt loop
    #[arg(long = "message")]
    pub messages: Vec<String>,

    /// Add a tool in app:operation form to each turn
    #[arg(long = "tool", value_parser = AgentToolArg::parse)]
    pub tools: Vec<AgentToolArg>,

    /// Optional execution budget in seconds for each created turn
    #[arg(long = "timeout-seconds", value_parser = clap::value_parser!(i32).range(1..))]
    pub timeout_seconds: Option<i32>,
}

#[derive(Subcommand)]
pub enum AgentCommands {
    /// Check the configured local agent harness
    Doctor(AgentDoctorArgs),
    /// Resume an interactive agent session
    Resume(AgentResumeArgs),
    /// Inspect and control agent sessions
    Sessions {
        #[command(subcommand)]
        command: AgentSessionCommands,
    },
    /// Inspect and control agent turns
    Turns {
        #[command(subcommand)]
        command: AgentTurnCommands,
    },
}

#[derive(Args)]
pub struct AgentDoctorArgs {
    /// Agent provider name; defaults to the configured default
    #[arg(long)]
    pub provider: Option<String>,

    /// Agent harness name; defaults to the server-selected harness
    #[arg(long)]
    pub harness: Option<String>,
}

#[derive(Args)]
pub struct AgentResumeArgs {
    /// Session ID to resume. Omit to resume the most recently updated active session.
    pub session_id: Option<String>,

    /// Provider filter when resuming the most recently updated active session
    #[arg(long, conflicts_with = "session_id")]
    pub provider: Option<String>,

    /// Model name override for future turns
    #[arg(long)]
    pub model: Option<String>,

    /// Add a system message to the first turn created in this CLI session
    #[arg(long = "system")]
    pub system: Vec<String>,

    /// Start with one or more user messages before entering the prompt loop
    #[arg(long = "message")]
    pub messages: Vec<String>,

    /// Add a tool in app:operation form to each turn
    #[arg(long = "tool", value_parser = AgentToolArg::parse)]
    pub tools: Vec<AgentToolArg>,

    /// Optional execution budget in seconds for each created turn
    #[arg(long = "timeout-seconds", value_parser = clap::value_parser!(i32).range(1..))]
    pub timeout_seconds: Option<i32>,
}

#[derive(Subcommand)]
pub enum WorkflowRunCommands {
    /// Cancel a workflow run
    Cancel {
        /// Run ID
        id: String,
        /// Optional cancellation reason
        #[arg(long)]
        reason: Option<String>,
    },
    /// Show a single workflow run
    Get {
        /// Run ID
        id: String,
    },
    /// List workflow runs
    List {
        /// Filter runs by target app
        #[arg(long)]
        app: Option<String>,
        /// Filter runs by status
        #[arg(long)]
        status: Option<String>,
        /// Number of runs to request
        #[arg(long)]
        page_size: Option<u32>,
        /// Token returned by a previous list response
        #[arg(long)]
        page_token: Option<String>,
    },
}

#[derive(Subcommand)]
pub enum AgentSessionCommands {
    /// Create an agent session
    Create(AgentSessionCreateArgs),
    /// Show a single agent session
    Get {
        /// Session ID
        id: String,
    },
    /// List agent sessions
    List {
        /// Filter sessions by provider
        #[arg(long)]
        provider: Option<String>,
        /// Filter sessions by state
        #[arg(long)]
        state: Option<String>,
        /// Maximum number of summary sessions to fetch
        #[arg(long, conflicts_with = "full")]
        limit: Option<usize>,
        /// Fetch the legacy full session list without summary pagination
        #[arg(long)]
        full: bool,
    },
    /// Update an existing agent session
    Update(AgentSessionUpdateArgs),
}

#[derive(Subcommand)]
pub enum AgentTurnCommands {
    /// Cancel an agent turn
    Cancel {
        /// Turn ID
        id: String,
        /// Optional cancellation reason
        #[arg(long)]
        reason: Option<String>,
    },
    /// Create an agent turn within a session
    Create(AgentTurnCreateArgs),
    /// Inspect or stream agent turn events
    Events {
        #[command(subcommand)]
        command: AgentTurnEventCommands,
    },
    /// Show a single agent turn
    Get {
        /// Turn ID
        id: String,
    },
    /// List turns in a session
    List {
        /// Session ID
        session_id: String,
        /// Filter turns by status
        #[arg(long)]
        status: Option<String>,
    },
    /// Render a stored turn as a transcript
    Transcript {
        /// Turn ID
        id: String,
    },
}

#[derive(Subcommand)]
pub enum AgentTurnEventCommands {
    /// List stored events for an agent turn
    List(AgentTurnEventListArgs),
    /// Stream events for an agent turn as server-sent events
    Stream(AgentTurnEventStreamArgs),
}

#[derive(Subcommand)]
pub enum WorkflowEventCommands {
    /// Deliver a workflow event
    Deliver(WorkflowEventDeliverArgs),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AgentToolArg {
    pub app: String,
    pub operation: String,
}

impl AgentToolArg {
    pub fn parse(input: &str) -> Result<Self, String> {
        let trimmed = input.trim();
        if trimmed.is_empty() {
            return Err("tool cannot be empty".to_string());
        }
        let (app, operation) = trimmed
            .split_once(':')
            .ok_or_else(|| format!("tool '{trimmed}' must use app:operation"))?;
        let app = app.trim();
        let operation = operation.trim();
        if app.is_empty() || operation.is_empty() {
            return Err(format!(
                "tool '{trimmed}' must include both app and operation"
            ));
        }
        Ok(Self {
            app: app.to_string(),
            operation: operation.to_string(),
        })
    }
}

#[derive(Args)]
pub struct AgentSessionCreateArgs {
    /// Agent provider name
    #[arg(long)]
    pub provider: Option<String>,

    /// Model name override
    #[arg(long)]
    pub model: Option<String>,

    /// Client reference for the session
    #[arg(long = "client-ref")]
    pub client_ref: Option<String>,

    /// Idempotency key for safe retries
    #[arg(long = "idempotency-key")]
    pub idempotency_key: Option<String>,

    /// Load the JSON request body from a file (use "-" for stdin)
    #[arg(long = "input", alias = "request-file")]
    pub input: Option<String>,
}

#[derive(Args)]
pub struct AgentSessionUpdateArgs {
    /// Session ID
    pub id: String,

    /// Client reference for the session
    #[arg(long = "client-ref")]
    pub client_ref: Option<String>,

    /// Session state
    #[arg(long)]
    pub state: Option<String>,

    /// Load the JSON request body from a file (use "-" for stdin)
    #[arg(long = "input", alias = "request-file")]
    pub input: Option<String>,
}

#[derive(Args)]
pub struct AgentTurnCreateArgs {
    /// Session ID
    pub session_id: String,

    /// Model name override
    #[arg(long)]
    pub model: Option<String>,

    /// Add a system message
    #[arg(long = "system")]
    pub system: Vec<String>,

    /// Add a user message
    #[arg(long = "message")]
    pub messages: Vec<String>,

    /// Add a tool in app:operation form
    #[arg(long = "tool", value_parser = AgentToolArg::parse)]
    pub tools: Vec<AgentToolArg>,

    /// Idempotency key for safe retries
    #[arg(long = "idempotency-key")]
    pub idempotency_key: Option<String>,

    /// Optional execution budget in seconds for the turn
    #[arg(long = "timeout-seconds", value_parser = clap::value_parser!(i32).range(1..))]
    pub timeout_seconds: Option<i32>,

    /// Load the JSON request body from a file (use "-" for stdin)
    #[arg(long = "input", alias = "request-file")]
    pub input: Option<String>,
}

#[derive(Args)]
pub struct AgentTurnEventListArgs {
    /// Turn ID
    pub id: String,

    /// Return events after this event sequence number
    #[arg(long)]
    pub after: Option<u64>,

    /// Maximum number of events to return
    #[arg(long)]
    pub limit: Option<u32>,
}

#[derive(Args)]
pub struct AgentTurnEventStreamArgs {
    /// Turn ID
    pub id: String,

    /// Stream events after this event sequence number
    #[arg(long)]
    pub after: Option<u64>,

    /// Maximum number of events to fetch per server poll
    #[arg(long)]
    pub limit: Option<u32>,
}

#[derive(Args)]
pub struct WorkflowEventDeliverArgs {
    /// Event type
    #[arg(long = "type")]
    pub event_type: String,

    /// Event source
    #[arg(long)]
    pub source: Option<String>,

    /// Event subject
    #[arg(long)]
    pub subject: Option<String>,

    /// Explicit event ID
    #[arg(long)]
    pub id: Option<String>,

    /// CloudEvents spec version
    #[arg(long = "spec-version")]
    pub spec_version: Option<String>,

    /// Event timestamp in RFC 3339 format
    #[arg(long)]
    pub time: Option<String>,

    /// Event data content type
    #[arg(long = "data-content-type")]
    pub data_content_type: Option<String>,

    /// Event data fields as key=value or key:=json
    #[arg(short = 'p', long = "data", value_parser = params::parse_param_entry)]
    pub data: Vec<params::ParamEntry>,

    /// Load event data from a JSON file (use "-" for stdin)
    #[arg(long = "data-file")]
    pub data_file: Option<String>,

    /// Event extension fields as key=value or key:=json
    #[arg(short = 'e', long = "extension", value_parser = params::parse_param_entry)]
    pub extensions: Vec<params::ParamEntry>,
}
