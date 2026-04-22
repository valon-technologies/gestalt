use clap::{Args, Parser, Subcommand};

use crate::output::Format;
use crate::params;

#[derive(Parser)]
#[command(name = "gestalt")]
#[command(about = "CLI for Gestalt API - authentication, plugins, and operations")]
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
    #[command(alias = "integrations")]
    Plugins {
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

    /// Manage workspace-owned identities
    Identities {
        #[command(subcommand)]
        command: IdentityCommands,
    },

    /// Manage workflow resources
    Workflows {
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
pub enum IdentityCommands {
    /// List identities you can access
    List,
    /// Create a new identity
    Create {
        /// Display name for the identity
        #[arg(long = "name")]
        display_name: String,
    },
    /// Show one identity
    Get {
        /// Identity ID
        id: String,
    },
    /// Update an identity's display name
    Update {
        /// Identity ID
        id: String,
        /// New display name
        #[arg(long = "name")]
        display_name: String,
    },
    /// Delete an identity
    Delete {
        /// Identity ID
        id: String,
    },
    /// Manage identity members
    Members {
        #[command(subcommand)]
        command: IdentityMemberCommands,
    },
    /// Manage identity grants
    Grants {
        #[command(subcommand)]
        command: IdentityGrantCommands,
    },
    /// Manage identity-owned API tokens
    Tokens {
        #[command(subcommand)]
        command: IdentityTokenCommands,
    },
}

#[derive(Subcommand)]
pub enum IdentityMemberCommands {
    /// List members for an identity
    List {
        /// Identity ID
        identity: String,
    },
    /// Add a member to an identity
    Add {
        /// Identity ID
        identity: String,
        /// User email
        email: String,
        /// Membership role
        #[arg(long, value_enum)]
        role: IdentityRole,
    },
    /// Update a member's role on an identity
    Update {
        /// Identity ID
        identity: String,
        /// User email
        email: String,
        /// Membership role
        #[arg(long, value_enum)]
        role: IdentityRole,
    },
    /// Remove a member from an identity
    Remove {
        /// Identity ID
        identity: String,
        /// User email
        email: String,
    },
}

#[derive(Subcommand)]
pub enum IdentityGrantCommands {
    /// List grants for an identity
    List {
        /// Identity ID
        identity: String,
    },
    /// Set or replace a plugin grant for an identity
    Set {
        /// Identity ID
        identity: String,
        /// Plugin name
        plugin: String,
        /// Restrict the grant to specific operations
        #[arg(long = "operation")]
        operations: Vec<String>,
    },
    /// Remove a plugin grant from an identity
    Revoke {
        /// Identity ID
        identity: String,
        /// Plugin name
        plugin: String,
    },
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct IdentityPermissionArg {
    pub plugin: String,
    pub operations: Vec<String>,
}

impl IdentityPermissionArg {
    pub fn parse(input: &str) -> Result<Self, String> {
        let trimmed = input.trim();
        if trimmed.is_empty() {
            return Err("permission cannot be empty".to_string());
        }

        let (plugin, operations) = match trimmed.split_once(':') {
            Some((plugin, operations)) => {
                let plugin = plugin.trim();
                if plugin.is_empty() {
                    return Err("permission plugin cannot be empty".to_string());
                }
                let operations: Vec<String> = operations
                    .split(',')
                    .map(str::trim)
                    .filter(|operation| !operation.is_empty())
                    .map(String::from)
                    .collect();
                if operations.is_empty() {
                    return Err(format!(
                        "permission '{trimmed}' must include at least one operation after ':'"
                    ));
                }
                (plugin.to_string(), operations)
            }
            None => (trimmed.to_string(), Vec::new()),
        };

        Ok(Self { plugin, operations })
    }
}

#[derive(Subcommand)]
pub enum IdentityTokenCommands {
    /// List API tokens for an identity
    List {
        /// Identity ID
        identity: String,
    },
    /// Create an API token for an identity
    Create {
        /// Identity ID
        identity: String,
        /// Display name for the token
        #[arg(long)]
        name: Option<String>,
        /// Permission entry in plugin or plugin:op1,op2 form
        #[arg(long = "permission", required = true, value_parser = IdentityPermissionArg::parse)]
        permissions: Vec<IdentityPermissionArg>,
    },
    /// Revoke an API token owned by an identity
    Revoke {
        /// Identity ID
        identity: String,
        /// Token ID
        id: String,
    },
}

#[derive(Subcommand)]
pub enum WorkflowCommands {
    /// Manage workflow schedules
    Schedules {
        #[command(subcommand)]
        command: WorkflowScheduleCommands,
    },
    /// Manage workflow event triggers
    Triggers {
        #[command(subcommand)]
        command: WorkflowTriggerCommands,
    },
    /// Inspect workflow runs
    Runs {
        #[command(subcommand)]
        command: WorkflowRunCommands,
    },
}

#[derive(Subcommand)]
pub enum WorkflowScheduleCommands {
    /// List workflow schedules
    List {
        /// Filter schedules by target plugin
        #[arg(long)]
        plugin: Option<String>,
    },
    /// Show a single workflow schedule
    Get {
        /// Schedule ID
        id: String,
    },
    /// Create a workflow schedule
    Create(WorkflowScheduleCreateArgs),
    /// Update an existing workflow schedule
    Update(WorkflowScheduleUpdateArgs),
    /// Delete a workflow schedule
    Delete {
        /// Schedule ID
        id: String,
    },
    /// Pause a workflow schedule
    Pause {
        /// Schedule ID
        id: String,
    },
    /// Resume a paused workflow schedule
    Resume {
        /// Schedule ID
        id: String,
    },
}

#[derive(Subcommand)]
pub enum WorkflowTriggerCommands {
    /// List workflow event triggers
    List {
        /// Filter event triggers by target plugin
        #[arg(long)]
        plugin: Option<String>,
        /// Filter event triggers by event type
        #[arg(long = "type")]
        event_type: Option<String>,
    },
    /// Show a single workflow event trigger
    Get {
        /// Trigger ID
        id: String,
    },
    /// Create a workflow event trigger
    Create(WorkflowTriggerCreateArgs),
    /// Update an existing workflow event trigger
    Update(WorkflowTriggerUpdateArgs),
    /// Delete a workflow event trigger
    Delete {
        /// Trigger ID
        id: String,
    },
    /// Pause a workflow event trigger
    Pause {
        /// Trigger ID
        id: String,
    },
    /// Resume a paused workflow event trigger
    Resume {
        /// Trigger ID
        id: String,
    },
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

#[derive(Args)]
pub struct WorkflowScheduleCreateArgs {
    /// Cron expression (e.g. "0 */5 * * *")
    #[arg(long)]
    pub cron: String,

    /// Target plugin (e.g. "slack", "github")
    #[arg(long)]
    pub plugin: String,

    /// Target operation (e.g. "chat.postMessage")
    #[arg(long)]
    pub operation: String,

    /// IANA timezone for the cron expression
    #[arg(long)]
    pub timezone: Option<String>,

    /// Select a named connection
    #[arg(long)]
    pub connection: Option<String>,

    /// Select a stored connection instance
    #[arg(long)]
    pub instance: Option<String>,

    /// Create the schedule in paused state
    #[arg(long)]
    pub paused: bool,

    /// Target input parameters as key=value or key:=json
    #[arg(short = 'p', long = "param", value_parser = params::parse_param_entry)]
    pub params: Vec<params::ParamEntry>,

    /// Load target input from a JSON file (use "-" for stdin)
    #[arg(long = "input-file")]
    pub input_file: Option<String>,
}

#[derive(Args)]
pub struct WorkflowTriggerCreateArgs {
    /// Event type to match exactly
    #[arg(long = "type")]
    pub event_type: String,

    /// Optional event source to match exactly
    #[arg(long)]
    pub source: Option<String>,

    /// Optional event subject to match exactly
    #[arg(long)]
    pub subject: Option<String>,

    /// Target plugin (e.g. "slack", "github")
    #[arg(long)]
    pub plugin: String,

    /// Target operation (e.g. "chat.postMessage")
    #[arg(long)]
    pub operation: String,

    /// Select a named connection
    #[arg(long)]
    pub connection: Option<String>,

    /// Select a stored connection instance
    #[arg(long)]
    pub instance: Option<String>,

    /// Create the trigger in paused state
    #[arg(long)]
    pub paused: bool,

    /// Target input parameters as key=value or key:=json
    #[arg(short = 'p', long = "param", value_parser = params::parse_param_entry)]
    pub params: Vec<params::ParamEntry>,

    /// Load target input from a JSON file (use "-" for stdin)
    #[arg(long = "input-file")]
    pub input_file: Option<String>,
}

#[derive(Args)]
pub struct WorkflowScheduleUpdateArgs {
    /// Schedule ID
    pub id: String,

    /// Cron expression (leave unset to keep existing)
    #[arg(long)]
    pub cron: Option<String>,

    /// Target plugin (leave unset to keep existing)
    #[arg(long)]
    pub plugin: Option<String>,

    /// Target operation (leave unset to keep existing)
    #[arg(long)]
    pub operation: Option<String>,

    /// IANA timezone (leave unset to keep existing; pass empty string to clear)
    #[arg(long)]
    pub timezone: Option<String>,

    /// Named connection (leave unset to keep existing; pass empty string to clear)
    #[arg(long)]
    pub connection: Option<String>,

    /// Stored connection instance (leave unset to keep existing; pass empty string to clear)
    #[arg(long)]
    pub instance: Option<String>,

    /// Mark the schedule as paused
    #[arg(long, conflicts_with = "unpaused", action = clap::ArgAction::SetTrue)]
    pub paused: bool,

    /// Mark the schedule as not paused
    #[arg(long = "no-paused", action = clap::ArgAction::SetTrue)]
    pub unpaused: bool,

    /// Replace target input with these key=value / key:=json entries
    #[arg(short = 'p', long = "param", value_parser = params::parse_param_entry)]
    pub params: Vec<params::ParamEntry>,

    /// Replace target input with the contents of this JSON file ("-" for stdin)
    #[arg(long = "input-file")]
    pub input_file: Option<String>,

    /// Clear the target input instead of keeping the existing value
    #[arg(long = "clear-input", conflicts_with_all = ["params", "input_file"])]
    pub clear_input: bool,
}

#[derive(Args)]
pub struct WorkflowTriggerUpdateArgs {
    /// Trigger ID
    pub id: String,

    /// Event type (leave unset to keep existing)
    #[arg(long = "type")]
    pub event_type: Option<String>,

    /// Event source (leave unset to keep existing; pass empty string to clear)
    #[arg(long)]
    pub source: Option<String>,

    /// Event subject (leave unset to keep existing; pass empty string to clear)
    #[arg(long)]
    pub subject: Option<String>,

    /// Target plugin (leave unset to keep existing)
    #[arg(long)]
    pub plugin: Option<String>,

    /// Target operation (leave unset to keep existing)
    #[arg(long)]
    pub operation: Option<String>,

    /// Named connection (leave unset to keep existing; pass empty string to clear)
    #[arg(long)]
    pub connection: Option<String>,

    /// Stored connection instance (leave unset to keep existing; pass empty string to clear)
    #[arg(long)]
    pub instance: Option<String>,

    /// Mark the event trigger as paused
    #[arg(long, conflicts_with = "unpaused", action = clap::ArgAction::SetTrue)]
    pub paused: bool,

    /// Mark the event trigger as not paused
    #[arg(long = "no-paused", action = clap::ArgAction::SetTrue)]
    pub unpaused: bool,

    /// Replace target input with these key=value / key:=json entries
    #[arg(short = 'p', long = "param", value_parser = params::parse_param_entry)]
    pub params: Vec<params::ParamEntry>,

    /// Replace target input with the contents of this JSON file ("-" for stdin)
    #[arg(long = "input-file")]
    pub input_file: Option<String>,

    /// Clear the target input instead of keeping the existing value
    #[arg(long = "clear-input", conflicts_with_all = ["params", "input_file"])]
    pub clear_input: bool,
}

#[derive(clap::ValueEnum, Clone, Copy, Debug, PartialEq, Eq)]
pub enum IdentityRole {
    Viewer,
    Editor,
    Admin,
}

impl IdentityRole {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Viewer => "viewer",
            Self::Editor => "editor",
            Self::Admin => "admin",
        }
    }
}
