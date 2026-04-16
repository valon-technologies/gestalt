use clap::{CommandFactory, Parser};
use gestalt::api::{self, ApiClient};
use gestalt::cli::{AuthCommands, Cli, Commands, ConfigCommands, PluginCommands, TokenCommands};
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
        Commands::Plugins { command } | Commands::Integrations { command } => {
            dispatch_plugin_command(command, url, format)
        }
        Commands::Invoke {
            plugin,
            operation,
            params,
            connection,
            instance,
            select,
            input_file,
        } => dispatch_plugin_command(
            PluginCommands::Invoke {
                plugin,
                operation,
                params,
                connection,
                instance,
                select,
                input_file,
            },
            url,
            format,
        ),
        Commands::Describe {
            plugin,
            operation,
            connection,
            instance,
        } => dispatch_plugin_command(
            PluginCommands::Describe {
                plugin,
                operation,
                connection,
                instance,
            },
            url,
            format,
        ),
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

fn dispatch_plugin_command(
    command: PluginCommands,
    url: Option<&str>,
    format: gestalt::output::Format,
) -> anyhow::Result<()> {
    let client = ApiClient::from_env(url)?;
    match command {
        PluginCommands::List => commands::plugins::list(&client, format),
        PluginCommands::Connect {
            name,
            connection,
            instance,
        } => commands::plugins::connect(&client, &name, connection.as_deref(), instance.as_deref()),
        PluginCommands::Disconnect {
            name,
            connection,
            instance,
        } => commands::plugins::disconnect(
            &client,
            &name,
            connection.as_deref(),
            instance.as_deref(),
        ),
        PluginCommands::Invoke {
            plugin,
            operation,
            params,
            connection,
            instance,
            select,
            input_file,
        } => commands::invoke::run(
            &client,
            &plugin,
            &operation,
            &params,
            commands::invoke::InvokeOptions {
                connection: connection.as_deref(),
                instance: instance.as_deref(),
                select: select.as_deref(),
                input_file: input_file.as_deref(),
            },
            format,
        ),
        PluginCommands::Describe {
            plugin,
            operation,
            connection,
            instance,
        } => commands::describe::describe(
            &client,
            &plugin,
            &operation,
            commands::describe::DescribeOptions {
                connection: connection.as_deref(),
                instance: instance.as_deref(),
            },
            format,
        ),
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
