use clap::{Args, Parser, Subcommand};

use crate::output::Format;
use crate::params;

#[derive(Parser)]
#[command(name = "gestalt")]
#[command(about = "CLI for Gestalt API - authentication, plugin, workflow, agent, and operations")]
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
    /// Manage authentication (login, logout)
    Auth {
        #[command(subcommand)]
        command: AuthCommands,
    },

    /// Interactive setup wizard
    Init,

    /// Manage persistent configuration
    Config {
        #[command(subcommand)]
        command: ConfigCommands,
    },

    /// Manage plugins
    #[command(aliases = ["plugins", "integrations"])]
    Plugin {
        #[command(subcommand)]
        command: PluginCommands,
    },

    #[command(hide = true)]
    /// Execute a plugin operation
    Invoke(InvokeArgs),

    #[command(hide = true)]
    /// Describe a plugin operation
    Describe(DescribeArgs),

    /// Manage API tokens
    Tokens {
        #[command(subcommand)]
        command: TokenCommands,
    },

    /// Manage workflow resources
    #[command(alias = "workflows")]
    Workflow {
        #[command(subcommand)]
        command: WorkflowCommands,
    },

    /// Run an interactive agent session or inspect agent resources
    Agent(AgentArgs),
}

#[derive(Subcommand)]
pub enum AuthCommands {
    /// Log in via browser OAuth flow
    Login,
    /// Log out and clear stored credentials
    Logout,
    /// Show authentication status
    Status,
}

#[derive(Subcommand)]
pub enum ConfigCommands {
    /// Get a config value
    Get {
        /// Config key
        key: String,
    },
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
    /// List all config values
    List,
}

#[derive(Subcommand)]
pub enum PluginCommands {
    /// List available plugins
    List,
    /// Connect a plugin via OAuth or interactive manual auth
    Connect {
        /// Plugin name (e.g., github, slack)
        name: String,

        /// Named connection to connect
        #[arg(long)]
        connection: Option<String>,

        /// Instance name to create or refresh
        #[arg(long)]
        instance: Option<String>,
    },
    /// Disconnect a plugin
    Disconnect {
        /// Plugin name (e.g., github, slack)
        name: String,

        /// Target a specific named connection
        #[arg(long)]
        connection: Option<String>,

        /// Target a specific stored instance
        #[arg(long)]
        instance: Option<String>,
    },
    /// Execute a plugin operation
    Invoke(InvokeArgs),
    /// Describe a plugin operation
    Describe(DescribeArgs),
}

#[derive(Args)]
pub struct InvokeArgs {
    /// Plugin name (e.g., github, slack)
    pub plugin: String,

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
    /// Plugin name
    pub plugin: String,
    /// Operation name
    pub operation: String,
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
    /// Publish workflow events
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

    /// Add a tool in plugin:operation form to each turn
    #[arg(long = "tool", value_parser = AgentToolArg::parse)]
    pub tools: Vec<AgentToolArg>,
}

#[derive(Subcommand)]
pub enum AgentCommands {
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

    /// Add a tool in plugin:operation form to each turn
    #[arg(long = "tool", value_parser = AgentToolArg::parse)]
    pub tools: Vec<AgentToolArg>,
}

#[derive(Subcommand)]
pub enum WorkflowRunCommands {
    /// List workflow runs
    List {
        /// Filter runs by target plugin
        #[arg(long)]
        plugin: Option<String>,
        /// Filter runs by status
        #[arg(long)]
        status: Option<String>,
    },
    /// Show a single workflow run
    Get {
        /// Run ID
        id: String,
    },
    /// Cancel a workflow run
    Cancel {
        /// Run ID
        id: String,
        /// Optional cancellation reason
        #[arg(long)]
        reason: Option<String>,
    },
}

#[derive(Subcommand)]
pub enum AgentSessionCommands {
    /// Create an agent session
    Create(AgentSessionCreateArgs),
    /// List agent sessions
    List {
        /// Filter sessions by provider
        #[arg(long)]
        provider: Option<String>,
        /// Filter sessions by state
        #[arg(long)]
        state: Option<String>,
    },
    /// Show a single agent session
    Get {
        /// Session ID
        id: String,
    },
    /// Update an existing agent session
    Update(AgentSessionUpdateArgs),
}

#[derive(Subcommand)]
pub enum AgentTurnCommands {
    /// Create an agent turn within a session
    Create(AgentTurnCreateArgs),
    /// List turns in a session
    List {
        /// Session ID
        session_id: String,
        /// Filter turns by status
        #[arg(long)]
        status: Option<String>,
    },
    /// Show a single agent turn
    Get {
        /// Turn ID
        id: String,
    },
    /// Render a stored turn as a transcript
    Transcript {
        /// Turn ID
        id: String,
    },
    /// Cancel an agent turn
    Cancel {
        /// Turn ID
        id: String,
        /// Optional cancellation reason
        #[arg(long)]
        reason: Option<String>,
    },
    /// Inspect or stream agent turn events
    Events {
        #[command(subcommand)]
        command: AgentTurnEventCommands,
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
    /// Publish a workflow event
    Publish(WorkflowEventPublishArgs),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AgentToolArg {
    pub plugin: String,
    pub operation: String,
}

impl AgentToolArg {
    pub fn parse(input: &str) -> Result<Self, String> {
        let trimmed = input.trim();
        if trimmed.is_empty() {
            return Err("tool cannot be empty".to_string());
        }
        let (plugin, operation) = trimmed
            .split_once(':')
            .ok_or_else(|| format!("tool '{trimmed}' must use plugin:operation"))?;
        let plugin = plugin.trim();
        let operation = operation.trim();
        if plugin.is_empty() || operation.is_empty() {
            return Err(format!(
                "tool '{trimmed}' must include both plugin and operation"
            ));
        }
        Ok(Self {
            plugin: plugin.to_string(),
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

    /// Add a tool in plugin:operation form
    #[arg(long = "tool", value_parser = AgentToolArg::parse)]
    pub tools: Vec<AgentToolArg>,

    /// Idempotency key for safe retries
    #[arg(long = "idempotency-key")]
    pub idempotency_key: Option<String>,

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
pub struct WorkflowEventPublishArgs {
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
