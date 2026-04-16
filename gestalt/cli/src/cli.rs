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
