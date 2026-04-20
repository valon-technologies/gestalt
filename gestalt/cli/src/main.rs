use clap::{CommandFactory, Parser};
use gestalt::api::{self, ApiClient};
use gestalt::cli::{
    AuthCommands, Cli, Commands, ConfigCommands, DescribeArgs, IdentityCommands,
    IdentityGrantCommands, IdentityMemberCommands, IdentityTokenCommands, InvokeArgs,
    PluginCommands, TokenCommands, WorkflowCommands, WorkflowScheduleCommands,
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
        Commands::Plugins { command } => dispatch_plugin_command(command, url, format),
        Commands::Invoke(args) => {
            dispatch_plugin_command(PluginCommands::Invoke(args), url, format)
        }
        Commands::Describe(args) => {
            dispatch_plugin_command(PluginCommands::Describe(args), url, format)
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
        Commands::Identities { command } => {
            let client = ApiClient::from_env(url)?;
            match command {
                IdentityCommands::List => commands::identities::list(&client, format),
                IdentityCommands::Create { display_name } => {
                    commands::identities::create(&client, &display_name, format)
                }
                IdentityCommands::Get { id } => commands::identities::get(&client, &id, format),
                IdentityCommands::Update { id, display_name } => {
                    commands::identities::update(&client, &id, &display_name, format)
                }
                IdentityCommands::Delete { id } => {
                    commands::identities::delete(&client, &id, format)
                }
                IdentityCommands::Members { command } => match command {
                    IdentityMemberCommands::List { identity } => {
                        commands::identities::list_members(&client, &identity, format)
                    }
                    IdentityMemberCommands::Add {
                        identity,
                        email,
                        role,
                    }
                    | IdentityMemberCommands::Update {
                        identity,
                        email,
                        role,
                    } => commands::identities::upsert_member(
                        &client,
                        &identity,
                        &email,
                        role.as_str(),
                        format,
                    ),
                    IdentityMemberCommands::Remove { identity, email } => {
                        commands::identities::remove_member(&client, &identity, &email, format)
                    }
                },
                IdentityCommands::Grants { command } => match command {
                    IdentityGrantCommands::List { identity } => {
                        commands::identities::list_grants(&client, &identity, format)
                    }
                    IdentityGrantCommands::Set {
                        identity,
                        plugin,
                        operations,
                    } => commands::identities::set_grant(
                        &client,
                        &identity,
                        &plugin,
                        &operations,
                        format,
                    ),
                    IdentityGrantCommands::Revoke { identity, plugin } => {
                        commands::identities::revoke_grant(&client, &identity, &plugin, format)
                    }
                },
                IdentityCommands::Tokens { command } => match command {
                    IdentityTokenCommands::List { identity } => {
                        commands::identities::list_tokens(&client, &identity, format)
                    }
                    IdentityTokenCommands::Create {
                        identity,
                        name,
                        permissions,
                    } => commands::identities::create_token(
                        &client,
                        &identity,
                        name.as_deref(),
                        &permissions,
                        format,
                    ),
                    IdentityTokenCommands::Revoke { identity, id } => {
                        commands::identities::revoke_token(&client, &identity, &id, format)
                    }
                },
            }
        }
        Commands::Workflows { command } => {
            let client = ApiClient::from_env(url)?;
            match command {
                WorkflowCommands::Schedules { command } => match command {
                    WorkflowScheduleCommands::List { plugin } => {
                        commands::workflows::list(&client, plugin.as_deref(), format)
                    }
                    WorkflowScheduleCommands::Get { id } => {
                        commands::workflows::get(&client, &id, format)
                    }
                    WorkflowScheduleCommands::Create(args) => {
                        commands::workflows::create(&client, &args, format)
                    }
                    WorkflowScheduleCommands::Update(args) => {
                        commands::workflows::update(&client, &args, format)
                    }
                    WorkflowScheduleCommands::Delete { id } => {
                        commands::workflows::delete(&client, &id, format)
                    }
                    WorkflowScheduleCommands::Pause { id } => {
                        commands::workflows::pause(&client, &id, format)
                    }
                    WorkflowScheduleCommands::Resume { id } => {
                        commands::workflows::resume(&client, &id, format)
                    }
                },
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
        PluginCommands::Invoke(InvokeArgs {
            plugin,
            operation,
            params,
            connection,
            instance,
            select,
            input_file,
        }) => commands::invoke::run(
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
        PluginCommands::Describe(DescribeArgs { plugin, operation }) => {
            commands::describe::describe(&client, &plugin, &operation, format)
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
