use clap::{Parser, Subcommand};

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
    Plugins {
        #[command(subcommand)]
        command: PluginCommands,
    },

    #[command(hide = true)]
    Integrations {
        #[command(subcommand)]
        command: PluginCommands,
    },

    #[command(hide = true)]
    /// Execute a plugin operation
    Invoke {
        /// Plugin name (e.g., github, slack)
        plugin: String,

        /// Operation name segments joined by "." (e.g., "chat postMessage" or "chat.postMessage"). Omit to list available operations.
        operation: Vec<String>,

        /// Parameters as key=value or key:=json pairs
        #[arg(short = 'p', long = "param", value_parser = params::parse_param_entry)]
        params: Vec<params::ParamEntry>,

        /// Select a named connection for this invocation
        #[arg(long)]
        connection: Option<String>,

        /// Select a stored connection instance
        #[arg(long)]
        instance: Option<String>,

        /// Select a sub-path from the response (e.g., "data.items")
        #[arg(long = "select")]
        select: Option<String>,

        /// Load parameters from a JSON file (use "-" for stdin)
        #[arg(long = "input-file")]
        input_file: Option<String>,
    },

    #[command(hide = true)]
    /// Describe a plugin operation
    Describe {
        /// Plugin name
        plugin: String,
        /// Operation name
        operation: String,
        /// Select a named connection when loading the operation contract
        #[arg(long)]
        connection: Option<String>,
        /// Select a stored connection instance when loading the operation contract
        #[arg(long)]
        instance: Option<String>,
    },

    /// Manage API tokens
    Tokens {
        #[command(subcommand)]
        command: TokenCommands,
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
    Invoke {
        /// Plugin name (e.g., github, slack)
        plugin: String,

        /// Operation name segments joined by "." (e.g., "chat postMessage" or "chat.postMessage"). Omit to list available operations.
        operation: Vec<String>,

        /// Parameters as key=value or key:=json pairs
        #[arg(short = 'p', long = "param", value_parser = params::parse_param_entry)]
        params: Vec<params::ParamEntry>,

        /// Select a named connection for this invocation
        #[arg(long)]
        connection: Option<String>,

        /// Select a stored connection instance
        #[arg(long)]
        instance: Option<String>,

        /// Select a sub-path from the response (e.g., "data.items")
        #[arg(long = "select")]
        select: Option<String>,

        /// Load parameters from a JSON file (use "-" for stdin)
        #[arg(long = "input-file")]
        input_file: Option<String>,
    },
    /// Describe a plugin operation
    Describe {
        /// Plugin name
        plugin: String,
        /// Operation name
        operation: String,
        /// Select a named connection when loading the operation contract
        #[arg(long)]
        connection: Option<String>,
        /// Select a stored connection instance when loading the operation contract
        #[arg(long)]
        instance: Option<String>,
    },
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
