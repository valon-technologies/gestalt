use clap::{CommandFactory, Parser};
use gestalt::api::{self, ApiClient};
use gestalt::cli::{
    AgentCommands, AgentSessionCommands, AgentTurnCommands, AgentTurnEventCommands, AuthCommands,
    Cli, Commands, ConfigCommands, DescribeArgs, InvokeArgs, PluginCommands, TokenCommands,
    WorkflowCommands, WorkflowEventCommands, WorkflowRunCommands, WorkflowScheduleCommands,
    WorkflowTriggerCommands,
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
        Commands::Plugin { command } => dispatch_plugin_command(command, url, format),
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
        Commands::Workflow { command } => {
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
                WorkflowCommands::Triggers { command } => match command {
                    WorkflowTriggerCommands::List { plugin, event_type } => {
                        commands::workflows::list_triggers(
                            &client,
                            plugin.as_deref(),
                            event_type.as_deref(),
                            format,
                        )
                    }
                    WorkflowTriggerCommands::Get { id } => {
                        commands::workflows::get_trigger(&client, &id, format)
                    }
                    WorkflowTriggerCommands::Create(args) => {
                        commands::workflows::create_trigger(&client, &args, format)
                    }
                    WorkflowTriggerCommands::Update(args) => {
                        commands::workflows::update_trigger(&client, &args, format)
                    }
                    WorkflowTriggerCommands::Delete { id } => {
                        commands::workflows::delete_trigger(&client, &id, format)
                    }
                    WorkflowTriggerCommands::Pause { id } => {
                        commands::workflows::pause_trigger(&client, &id, format)
                    }
                    WorkflowTriggerCommands::Resume { id } => {
                        commands::workflows::resume_trigger(&client, &id, format)
                    }
                },
                WorkflowCommands::Runs { command } => match command {
                    WorkflowRunCommands::List { plugin, status } => commands::workflows::list_runs(
                        &client,
                        plugin.as_deref(),
                        status.as_deref(),
                        format,
                    ),
                    WorkflowRunCommands::Get { id } => {
                        commands::workflows::get_run(&client, &id, format)
                    }
                    WorkflowRunCommands::Cancel { id, reason } => {
                        commands::workflows::cancel_run(&client, &id, reason.as_deref(), format)
                    }
                },
                WorkflowCommands::Events { command } => match command {
                    WorkflowEventCommands::Publish(args) => {
                        commands::workflows::publish_event(&client, &args, format)
                    }
                },
            }
        }
        Commands::Agent { command } => {
            let client = ApiClient::from_env(url)?;
            match command {
                AgentCommands::Sessions { command } => match command {
                    AgentSessionCommands::Create(args) => {
                        commands::agents::create_session(&client, &args, format)
                    }
                    AgentSessionCommands::List { provider, state } => {
                        commands::agents::list_sessions(
                            &client,
                            provider.as_deref(),
                            state.as_deref(),
                            format,
                        )
                    }
                    AgentSessionCommands::Get { id } => {
                        commands::agents::get_session(&client, &id, format)
                    }
                    AgentSessionCommands::Update(args) => {
                        commands::agents::update_session(&client, &args, format)
                    }
                },
                AgentCommands::Turns { command } => match command {
                    AgentTurnCommands::Create(args) => {
                        commands::agents::create_turn(&client, &args, format)
                    }
                    AgentTurnCommands::List { session_id, status } => commands::agents::list_turns(
                        &client,
                        &session_id,
                        status.as_deref(),
                        format,
                    ),
                    AgentTurnCommands::Get { id } => {
                        commands::agents::get_turn(&client, &id, format)
                    }
                    AgentTurnCommands::Cancel { id, reason } => {
                        commands::agents::cancel_turn(&client, &id, reason.as_deref(), format)
                    }
                    AgentTurnCommands::Events { command } => match command {
                        AgentTurnEventCommands::List(args) => {
                            commands::agents::list_turn_events(&client, &args, format)
                        }
                        AgentTurnEventCommands::Stream(args) => {
                            commands::agents::stream_turn_events(&client, &args)
                        }
                    },
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
