use clap::{CommandFactory, Parser};
use gestalt::api::{self, ApiClient};
use gestalt::cli::{
    AgentArgs, AgentCommands, AgentSessionCommands, AgentTurnCommands, AgentTurnEventCommands,
    AppCommands, AuthCommands, Cli, Commands, ConfigCommands, DescribeArgs, InvokeArgs,
    TokenCommands,
};
use gestalt::commands;
use gestalt::output;

fn run() -> anyhow::Result<()> {
    let cli = Cli::parse();
    let format = cli.format;
    let url = cli.url.as_deref();
    let url_was_explicit = cli.url.is_some();

    let command = match cli.command {
        Some(cmd) => cmd,
        None => return print_help_with_context(url),
    };

    match command {
        Commands::Auth { command } => match command {
            AuthCommands::Login => commands::auth::login(url),
            AuthCommands::Logout => commands::auth::logout(),
            AuthCommands::Status => commands::auth::status(url, format),
            AuthCommands::Token { command } => dispatch_token_command(command, url, format),
        },
        Commands::Init => commands::init::run(url),
        Commands::Config { command } => match command {
            ConfigCommands::Get { key } => commands::config::get(&key, format),
            ConfigCommands::Set { key, value } => commands::config::set(&key, &value),
            ConfigCommands::Unset { key } => commands::config::unset(&key),
            ConfigCommands::List => commands::config::list(format),
        },
        Commands::Authorization { command } => {
            let api = ApiClient::from_env(url)?;
            let transport = gestalt_sdk::public::grpc_transport::SyncGrpcTransport::from_endpoint(
                gestalt_sdk::public::grpc_transport::dial_public_grpc(api.base_url())?,
                std::sync::Arc::new(gestalt_sdk::public::auth::BearerAuth::new(api.token())),
            )
            .with_timeout(std::time::Duration::from_secs(30));
            let authz =
                gestalt_sdk::public::generated::app_client::AuthorizationClient::new(transport);
            commands::authorization::dispatch(&authz, command, format)
        }
        Commands::App { command } => dispatch_app_command(command, url, format),
        Commands::Invoke(args) => dispatch_app_command(AppCommands::Invoke(args), url, format),
        Commands::Describe(args) => dispatch_app_command(AppCommands::Describe(args), url, format),
        Commands::Workflow { command } => {
            let api = ApiClient::from_env(url)?;
            let transport = gestalt_sdk::public::grpc_transport::SyncGrpcTransport::from_endpoint(
                gestalt_sdk::public::grpc_transport::dial_public_grpc(api.base_url())?,
                std::sync::Arc::new(gestalt_sdk::public::auth::BearerAuth::new(api.token())),
            )
            .with_timeout(std::time::Duration::from_secs(30));
            let workflow =
                gestalt_sdk::public::generated::app_client::WorkflowClient::new(transport);
            commands::workflows::dispatch(&api, &workflow, command, format)
        }
        Commands::Agent(args) => dispatch_agent(args, url, url_was_explicit, format),
    }
}

fn dispatch_token_command(
    command: TokenCommands,
    url: Option<&str>,
    format: gestalt::output::Format,
) -> anyhow::Result<()> {
    let client = ApiClient::from_env(url)?;
    match command {
        TokenCommands::Create { scopes, expires_in } => {
            commands::tokens::create(&client, &scopes, expires_in, format)
        }
        TokenCommands::List => commands::tokens::list(&client, format),
        TokenCommands::Revoke { id } => commands::tokens::revoke(&client, &id, format),
    }
}

fn dispatch_agent(
    args: AgentArgs,
    url: Option<&str>,
    _url_was_explicit: bool,
    format: gestalt::output::Format,
) -> anyhow::Result<()> {
    if args.command.is_some() {
        reject_agent_interactive_options(&args)?;
        let client = ApiClient::from_env(url)?;
        return match args.command {
            Some(AgentCommands::Resume(resume)) => {
                commands::agents::resume_interactive(&client, &resume)
            }
            Some(command) => dispatch_agent_command(&client, command, format),
            None => unreachable!(),
        };
    }
    let client = ApiClient::from_env(url)?;
    commands::agents::run_interactive(&client, &args)
}

fn reject_agent_interactive_options(args: &AgentArgs) -> anyhow::Result<()> {
    if args.model.is_some() {
        anyhow::bail!("--model must be passed before a prompt or after `agent resume`");
    }
    if !args.system.is_empty() {
        anyhow::bail!("--system must be passed before a prompt or after `agent resume`");
    }
    if !args.messages.is_empty() {
        anyhow::bail!("--message must be passed before a prompt or after `agent resume`");
    }
    if !args.tools.is_empty() {
        anyhow::bail!("--tool must be passed before a prompt or after `agent resume`");
    }
    if args.timeout_seconds.is_some() {
        anyhow::bail!("--timeout-seconds must be passed before a prompt or after `agent resume`");
    }
    if args.provider.is_some() {
        anyhow::bail!("--provider must be passed before a prompt or after `agent resume`");
    }
    Ok(())
}

fn dispatch_agent_command(
    client: &ApiClient,
    command: AgentCommands,
    format: gestalt::output::Format,
) -> anyhow::Result<()> {
    match command {
        AgentCommands::Resume(_) => anyhow::bail!("agent resume is interactive"),
        AgentCommands::Sessions { command } => match command {
            AgentSessionCommands::Create(args) => {
                commands::agents::create_session(client, &args, format)
            }
            AgentSessionCommands::List {
                provider,
                state,
                limit,
                full,
            } => commands::agents::list_sessions(
                client,
                provider.as_deref(),
                state.as_deref(),
                limit,
                full,
                format,
            ),
            AgentSessionCommands::Get { id } => commands::agents::get_session(client, &id, format),
            AgentSessionCommands::Update(args) => {
                commands::agents::update_session(client, &args, format)
            }
        },
        AgentCommands::Turns { command } => match command {
            AgentTurnCommands::Create(args) => commands::agents::create_turn(client, &args, format),
            AgentTurnCommands::List { session_id, status } => {
                commands::agents::list_turns(client, &session_id, status.as_deref(), format)
            }
            AgentTurnCommands::Get { id } => commands::agents::get_turn(client, &id, format),
            AgentTurnCommands::Transcript { id } => {
                commands::agents::transcript_turn(client, &id, format)
            }
            AgentTurnCommands::Cancel { id, reason } => {
                commands::agents::cancel_turn(client, &id, reason.as_deref(), format)
            }
            AgentTurnCommands::Events { command } => match command {
                AgentTurnEventCommands::List(args) => {
                    commands::agents::list_turn_events(client, &args, format)
                }
            },
        },
    }
}

fn dispatch_app_command(
    command: AppCommands,
    url: Option<&str>,
    format: gestalt::output::Format,
) -> anyhow::Result<()> {
    let client = ApiClient::from_env(url)?;
    match command {
        AppCommands::List => commands::apps::list(&client, format),
        AppCommands::Connect {
            name,
            connection,
            instance,
            service_account_id,
        } => commands::apps::connect(
            &client,
            &name,
            connection.as_deref(),
            instance.as_deref(),
            service_account_id.as_deref(),
        ),
        AppCommands::Disconnect {
            name,
            connection,
            instance,
        } => commands::apps::disconnect(&client, &name, connection.as_deref(), instance.as_deref()),
        AppCommands::Invoke(InvokeArgs {
            app,
            operation,
            params,
            connection,
            instance,
            select,
            input_file,
        }) => commands::invoke::run(
            &client,
            &app,
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
        AppCommands::Describe(DescribeArgs {
            app,
            operation,
            connection,
            instance,
        }) => commands::describe::describe(
            &client,
            &app,
            &operation,
            connection.as_deref(),
            instance.as_deref(),
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
