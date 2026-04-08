use clap::{CommandFactory, Parser};
use gestalt::api::{self, ApiClient};
use gestalt::cli::{
    AuthCommands, Cli, Commands, ConfigCommands, IntegrationCommands, TokenCommands,
};
use gestalt::commands;
use gestalt::output;

fn run() -> anyhow::Result<()> {
    let cli = Cli::parse();
    let format = cli.format;
    let url = cli.url.as_deref();

    let command = match cli.command {
        Some(cmd) => cmd,
        None => return print_help_with_context(url),
    };

    match command {
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
        Commands::Integrations { command } => {
            let client = ApiClient::from_env(url)?;
            match command {
                IntegrationCommands::List => commands::integrations::list(&client, format),
                IntegrationCommands::Connect {
                    name,
                    connection,
                    instance,
                } => commands::integrations::connect(
                    &client,
                    &name,
                    connection.as_deref(),
                    instance.as_deref(),
                ),
                IntegrationCommands::Disconnect {
                    name,
                    connection,
                    instance,
                } => commands::integrations::disconnect(
                    &client,
                    &name,
                    connection.as_deref(),
                    instance.as_deref(),
                ),
            }
        }
        Commands::Invoke {
            integration,
            operation,
            params,
            connection,
            instance,
            select,
            input_file,
        } => {
            let client = ApiClient::from_env(url)?;
            match operation {
                Some(op) => commands::invoke::invoke(
                    &client,
                    &integration,
                    &op,
                    &params,
                    commands::invoke::InvokeOptions {
                        connection: connection.as_deref(),
                        instance: instance.as_deref(),
                        select: select.as_deref(),
                        input_file: input_file.as_deref(),
                    },
                    format,
                ),
                None => {
                    if !params.is_empty() {
                        output::print_warning("parameters ignored; no operation specified");
                    }
                    commands::invoke::list_operations(&client, &integration, format)
                }
            }
        }
        Commands::Describe {
            integration,
            operation,
        } => {
            let client = ApiClient::from_env(url)?;
            commands::describe::describe(&client, &integration, &operation, format)
        }
        Commands::Tokens { command } => {
            let client = ApiClient::from_env(url)?;
            match command {
                TokenCommands::Create { name } => {
                    commands::tokens::create(&client, name.as_deref(), format)
                }
                TokenCommands::List => commands::tokens::list(&client, format),
                TokenCommands::Revoke { id } => commands::tokens::revoke(&client, &id, format),
            }
        }
    }
}

fn print_help_with_context(url_override: Option<&str>) -> anyhow::Result<()> {
    Cli::command().print_help()?;
    eprintln!();
    match api::describe_server_config(url_override) {
        Some((server_url, source)) => {
            eprintln!("Target server: {server_url}");
            eprintln!("Config source: {source}");
        }
        None => {
            eprintln!("Target server: not configured");
        }
    }
    Ok(())
}

fn main() {
    if let Err(e) = run() {
        output::print_error(&format!("{:#}", e));
        std::process::exit(1);
    }
}
