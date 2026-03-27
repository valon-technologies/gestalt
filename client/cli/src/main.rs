use clap::{Parser, Subcommand};
use gestalt::commands;
use gestalt::output::{self, Format};

#[derive(Parser)]
#[command(name = "gestalt")]
#[command(about = "CLI for Gestalt API - authentication, integrations, and operations")]
#[command(version)]
struct Cli {
    #[command(subcommand)]
    command: Commands,

    /// Output format
    #[arg(long, global = true, value_enum, default_value_t = Format::Table)]
    format: Format,

    /// API server URL (overrides config and env)
    #[arg(long, global = true)]
    url: Option<String>,
}

#[derive(Subcommand)]
enum Commands {
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

    /// Manage third-party integrations
    Integrations {
        #[command(subcommand)]
        command: IntegrationCommands,
    },

    /// Execute an integration operation
    Invoke {
        /// Integration name (e.g., github, slack)
        integration: String,

        /// Operation name (e.g., search_code, list_channels). Omit to list available operations.
        operation: Option<String>,

        /// Parameters as key=value or key:=json pairs
        #[arg(short = 'p', long = "param", value_parser = gestalt::params::parse_param_entry)]
        params: Vec<gestalt::params::ParamEntry>,

        /// Select a sub-path from the response (e.g., "data.items")
        #[arg(long = "select")]
        select: Option<String>,

        /// Load parameters from a JSON file (use "-" for stdin)
        #[arg(long = "input-file")]
        input_file: Option<String>,
    },

    /// Describe an integration operation
    Describe {
        /// Integration name
        integration: String,
        /// Operation name
        operation: String,
    },

    /// Manage API tokens
    Tokens {
        #[command(subcommand)]
        command: TokenCommands,
    },
}

#[derive(Subcommand)]
enum AuthCommands {
    /// Log in via browser OAuth flow
    Login,
    /// Log out and clear stored credentials
    Logout,
    /// Show authentication status
    Status,
}

#[derive(Subcommand)]
enum ConfigCommands {
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
enum IntegrationCommands {
    /// List available integrations
    List,
    /// Connect an integration via OAuth
    Connect {
        /// Integration name (e.g., github, slack)
        name: String,
    },
}

#[derive(Subcommand)]
enum TokenCommands {
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

fn main() {
    let cli = Cli::parse();
    let format = cli.format;
    let url = cli.url.as_deref();

    let result = match cli.command {
        Commands::Auth { command } => match command {
            AuthCommands::Login => commands::auth::login(url),
            AuthCommands::Logout => commands::auth::logout(),
            AuthCommands::Status => commands::auth::status(url, format),
        },
        Commands::Init => commands::init::run(url),
        Commands::Config { command } => match command {
            ConfigCommands::Get { key } => commands::config::get(&key, format),
            ConfigCommands::Set { key, value } => commands::config::set(&key, &value),
            ConfigCommands::Unset { key } => commands::config::unset(&key),
            ConfigCommands::List => commands::config::list(format),
        },
        Commands::Integrations { command } => match command {
            IntegrationCommands::List => commands::integrations::list(url, format),
            IntegrationCommands::Connect { name } => commands::integrations::connect(url, &name),
        },
        Commands::Invoke {
            integration,
            operation,
            params,
            select,
            input_file,
        } => match operation {
            Some(op) => commands::invoke::invoke(
                url,
                &integration,
                &op,
                &params,
                select.as_deref(),
                input_file.as_deref(),
                format,
            ),
            None => {
                if !params.is_empty() {
                    output::print_warning("parameters ignored; no operation specified");
                }
                commands::invoke::list_operations(url, &integration, format)
            }
        },
        Commands::Describe {
            integration,
            operation,
        } => commands::describe::describe(url, &integration, &operation, format),
        Commands::Tokens { command } => match command {
            TokenCommands::Create { name } => {
                commands::tokens::create(url, name.as_deref(), format)
            }
            TokenCommands::List => commands::tokens::list(url, format),
            TokenCommands::Revoke { id } => commands::tokens::revoke(url, &id, format),
        },
    };

    if let Err(e) = result {
        output::print_error(&format!("{:#}", e));
        std::process::exit(1);
    }
}
