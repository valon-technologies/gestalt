use clap::{Args, Parser, Subcommand};

use crate::output::Format;
use crate::params;

#[derive(Parser)]
#[command(name = "gestalt")]
#[command(about = "CLI for Gestalt API - authentication, app, workflow, agent, and operations")]
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

    /// Manage apps
    #[command(alias = "apps")]
    App {
        #[command(subcommand)]
        command: AppCommands,
    },

    #[command(hide = true)]
    /// Execute an app operation
    Invoke(InvokeArgs),

    #[command(hide = true)]
    /// Describe an app operation
    Describe(DescribeArgs),

    /// Manage API tokens
    Tokens {
        #[command(subcommand)]
        command: TokenCommands,
    },

    /// Manage authorization resources
    #[command(name = "authz", alias = "authorization")]
    Authorization {
        #[command(subcommand)]
        command: AuthorizationCommands,
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
pub enum AppCommands {
    /// List available apps
    List,
    /// Connect an app via OAuth or interactive manual auth
    Connect {
        /// App name (e.g., github, slack)
        name: String,

        /// Named connection to connect
        #[arg(long)]
        connection: Option<String>,

        /// Instance name to create or refresh
        #[arg(long)]
        instance: Option<String>,
    },
    /// Disconnect an app
    Disconnect {
        /// App name (e.g., github, slack)
        name: String,

        /// Target a specific named connection
        #[arg(long)]
        connection: Option<String>,

        /// Target a specific stored instance
        #[arg(long)]
        instance: Option<String>,
    },
    /// Execute an app operation
    Invoke(InvokeArgs),
    /// Describe an app operation
    Describe(DescribeArgs),
}

#[derive(Args)]
pub struct InvokeArgs {
    /// App name (e.g., github, slack)
    pub app: String,

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
    /// App name
    pub app: String,
    /// Operation name
    pub operation: String,
    /// Select a named connection for this operation catalog
    #[arg(long)]
    pub connection: Option<String>,
    /// Select a stored connection instance for this operation catalog
    #[arg(long)]
    pub instance: Option<String>,
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
pub enum AuthorizationCommands {
    /// Manage service-account subjects
    Subjects {
        #[command(subcommand)]
        command: AuthorizationSubjectCommands,
    },
    /// Manage app authorization memberships
    Apps {
        #[command(subcommand)]
        command: AuthorizationAppCommands,
    },
    /// Manage built-in admin authorization memberships
    Admins {
        #[command(subcommand)]
        command: AuthorizationAdminCommands,
    },
    /// Inspect the configured authorization provider
    Provider {
        #[command(subcommand)]
        command: AuthorizationProviderCommands,
    },
    /// Inspect authorization models
    Models {
        #[command(subcommand)]
        command: AuthorizationModelCommands,
    },
    /// Inspect authorization relationships
    Relationships {
        #[command(subcommand)]
        command: AuthorizationRelationshipCommands,
    },
}

#[derive(Subcommand)]
pub enum AuthorizationSubjectCommands {
    /// List service-account subjects
    List,
    /// Create a service-account subject
    Create(AuthorizationSubjectCreateArgs),
    /// Show a service-account subject
    Get {
        /// Service-account slug or canonical service_account:<id>
        subject: String,
    },
    /// Update a service-account subject
    Update(AuthorizationSubjectUpdateArgs),
    /// Delete a service-account subject
    Delete {
        /// Service-account slug or canonical service_account:<id>
        subject: String,
    },
    /// Manage subject administrators and editors
    Members {
        #[command(subcommand)]
        command: AuthorizationSubjectMemberCommands,
    },
    /// Manage subject app grants
    Grants {
        #[command(subcommand)]
        command: AuthorizationSubjectGrantCommands,
    },
    /// Manage external identity assumptions
    ExternalIdentities {
        #[command(subcommand)]
        command: AuthorizationSubjectExternalIdentityCommands,
    },
    /// Manage subject-owned app credentials
    Integrations {
        #[command(subcommand)]
        command: AuthorizationSubjectIntegrationCommands,
    },
    /// Manage subject-owned API tokens
    Tokens {
        #[command(subcommand)]
        command: AuthorizationSubjectTokenCommands,
    },
}

#[derive(Args)]
pub struct AuthorizationSubjectCreateArgs {
    /// Service-account slug or canonical service_account:<id>
    pub subject: String,

    /// Display name
    #[arg(long = "display-name")]
    pub display_name: Option<String>,

    /// Description
    #[arg(long)]
    pub description: Option<String>,
}

#[derive(Args)]
pub struct AuthorizationSubjectUpdateArgs {
    /// Service-account slug or canonical service_account:<id>
    pub subject: String,

    /// Display name
    #[arg(long = "display-name")]
    pub display_name: Option<String>,

    /// Description
    #[arg(long)]
    pub description: Option<String>,
}

#[derive(Debug, Clone, Copy, clap::ValueEnum)]
pub enum AuthorizationManagedSubjectRole {
    Viewer,
    Editor,
    Admin,
}

#[derive(Subcommand)]
pub enum AuthorizationSubjectMemberCommands {
    /// List members that can manage a subject
    List {
        /// Service-account slug or canonical service_account:<id>
        subject: String,
    },
    /// Add or update a subject member
    Set(AuthorizationSubjectMemberSetArgs),
    /// Remove a subject member
    Remove {
        /// Service-account slug or canonical service_account:<id>
        subject: String,
        /// Canonical member subject ID
        member_subject_id: String,
    },
}

#[derive(Args)]
pub struct AuthorizationSubjectMemberSetArgs {
    /// Service-account slug or canonical service_account:<id>
    pub subject: String,

    /// Canonical member subject ID
    #[arg(long = "subject-id", conflicts_with = "email")]
    pub subject_id: Option<String>,

    /// User email alias
    #[arg(long, conflicts_with = "subject_id")]
    pub email: Option<String>,

    /// Member role
    #[arg(long, value_enum)]
    pub role: AuthorizationManagedSubjectRole,
}

#[derive(Subcommand)]
pub enum AuthorizationSubjectGrantCommands {
    /// List app grants for a subject
    List {
        /// Service-account slug or canonical service_account:<id>
        subject: String,
    },
    /// Grant an app role to a subject
    Set {
        /// Service-account slug or canonical service_account:<id>
        subject: String,
        /// App name
        app: String,
        /// App role
        #[arg(long)]
        role: String,
    },
    /// Remove an app grant from a subject
    Remove {
        /// Service-account slug or canonical service_account:<id>
        subject: String,
        /// App name
        app: String,
    },
}

#[derive(Subcommand)]
pub enum AuthorizationSubjectExternalIdentityCommands {
    /// List external identities assumed by a subject
    List {
        /// Service-account slug or canonical service_account:<id>
        subject: String,
    },
    /// Allow a subject to assume an external identity
    Add(AuthorizationSubjectExternalIdentityArgs),
    /// Remove an external identity assumption
    Remove(AuthorizationSubjectExternalIdentityArgs),
}

#[derive(Args)]
pub struct AuthorizationSubjectExternalIdentityArgs {
    /// Service-account slug or canonical service_account:<id>
    pub subject: String,

    /// External identity type
    #[arg(long = "type")]
    pub identity_type: String,

    /// External identity ID
    #[arg(long)]
    pub id: String,
}

#[derive(Subcommand)]
pub enum AuthorizationSubjectIntegrationCommands {
    /// List app credential state for a subject
    List {
        /// Service-account slug or canonical service_account:<id>
        subject: String,
    },
    /// Connect an app credential for a subject
    Connect {
        /// Service-account slug or canonical service_account:<id>
        subject: String,
        /// App name
        name: String,
        /// Named connection to connect
        #[arg(long)]
        connection: Option<String>,
        /// Instance name to create or refresh
        #[arg(long)]
        instance: Option<String>,
    },
    /// Disconnect an app credential from a subject
    Disconnect {
        /// Service-account slug or canonical service_account:<id>
        subject: String,
        /// App name
        name: String,
        /// Target a specific named connection
        #[arg(long)]
        connection: Option<String>,
        /// Target a specific stored instance
        #[arg(long)]
        instance: Option<String>,
    },
}

#[derive(Subcommand)]
pub enum AuthorizationSubjectTokenCommands {
    /// List API tokens owned by a subject
    List {
        /// Service-account slug or canonical service_account:<id>
        subject: String,
    },
    /// Create a subject-owned API token
    Create(AuthorizationSubjectTokenCreateArgs),
    /// Revoke one subject-owned API token
    Revoke {
        /// Service-account slug or canonical service_account:<id>
        subject: String,
        /// Token ID
        id: String,
    },
    /// Revoke all subject-owned API tokens
    RevokeAll {
        /// Service-account slug or canonical service_account:<id>
        subject: String,
    },
}

#[derive(Args)]
pub struct AuthorizationSubjectTokenCreateArgs {
    /// Service-account slug or canonical service_account:<id>
    pub subject: String,

    /// Token name
    #[arg(long)]
    pub name: String,

    /// Legacy token scopes
    #[arg(long, conflicts_with_all = ["permission", "action", "permissions_file"])]
    pub scopes: Option<String>,

    /// Operation permission in app:operation form
    #[arg(long = "permission", conflicts_with = "permissions_file")]
    pub permission: Vec<String>,

    /// Action permission in app:action form
    #[arg(long = "action", conflicts_with = "permissions_file")]
    pub action: Vec<String>,

    /// JSON file containing a raw permissions array
    #[arg(long = "permissions-file", conflicts_with_all = ["permission", "action", "scopes"])]
    pub permissions_file: Option<String>,
}

#[derive(Subcommand)]
pub enum AuthorizationAppCommands {
    /// List apps that declare authorization policies
    List,
    /// Manage app members
    Members {
        #[command(subcommand)]
        command: AuthorizationAppMemberCommands,
    },
}

#[derive(Subcommand)]
pub enum AuthorizationAppMemberCommands {
    /// List app members
    List {
        /// App name
        app: String,
    },
    /// Add or update an app member
    Set(AuthorizationAppMemberSetArgs),
    /// Remove an app member
    Remove {
        /// App name
        app: String,
        /// Canonical subject ID
        subject_id: String,
    },
}

#[derive(Args)]
pub struct AuthorizationAppMemberSetArgs {
    /// App name
    pub app: String,

    /// Canonical subject ID
    #[arg(long = "subject-id", conflicts_with = "email")]
    pub subject_id: Option<String>,

    /// User email alias
    #[arg(long, conflicts_with = "subject_id")]
    pub email: Option<String>,

    /// App role
    #[arg(long)]
    pub role: String,
}

#[derive(Subcommand)]
pub enum AuthorizationAdminCommands {
    /// Manage built-in admin members
    Members {
        #[command(subcommand)]
        command: AuthorizationAdminMemberCommands,
    },
}

#[derive(Subcommand)]
pub enum AuthorizationAdminMemberCommands {
    /// List admin members
    List,
    /// Add or update an admin member
    Set(AuthorizationAdminMemberSetArgs),
    /// Remove an admin member
    Remove {
        /// Canonical subject ID
        subject_id: String,
    },
}

#[derive(Args)]
pub struct AuthorizationAdminMemberSetArgs {
    /// Canonical subject ID
    #[arg(long = "subject-id", conflicts_with = "email")]
    pub subject_id: Option<String>,

    /// User email alias
    #[arg(long, conflicts_with = "subject_id")]
    pub email: Option<String>,

    /// Admin role
    #[arg(long)]
    pub role: String,
}

#[derive(Subcommand)]
pub enum AuthorizationProviderCommands {
    /// Show authorization provider metadata
    Get,
}

#[derive(Subcommand)]
pub enum AuthorizationModelCommands {
    /// List authorization models
    List(AuthorizationPageArgs),
}

#[derive(Subcommand)]
pub enum AuthorizationRelationshipCommands {
    /// List authorization relationships
    List(AuthorizationRelationshipListArgs),
}

#[derive(Args, Default)]
pub struct AuthorizationPageArgs {
    /// Page size
    #[arg(long = "page-size")]
    pub page_size: Option<u32>,

    /// Page token
    #[arg(long = "page-token")]
    pub page_token: Option<String>,
}

#[derive(Args, Default)]
pub struct AuthorizationRelationshipListArgs {
    /// Subject type filter
    #[arg(long = "subject-type")]
    pub subject_type: Option<String>,

    /// Subject ID filter
    #[arg(long = "subject-id")]
    pub subject_id: Option<String>,

    /// Relation filter
    #[arg(long)]
    pub relation: Option<String>,

    /// Resource type filter
    #[arg(long = "resource-type")]
    pub resource_type: Option<String>,

    /// Resource ID filter
    #[arg(long = "resource-id")]
    pub resource_id: Option<String>,

    /// Model ID filter
    #[arg(long = "model-id")]
    pub model_id: Option<String>,

    /// Page size
    #[arg(long = "page-size")]
    pub page_size: Option<u32>,

    /// Page token
    #[arg(long = "page-token")]
    pub page_token: Option<String>,
}

#[derive(Subcommand)]
pub enum WorkflowCommands {
    /// Manage workflow schedules
    Schedules {
        #[command(subcommand)]
        command: WorkflowScheduleCommands,
    },
    /// Manage workflow triggers
    Triggers {
        #[command(subcommand)]
        command: WorkflowTriggerCommands,
    },
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

    /// Run the agent harness locally (default for `gestalt agent`)
    #[arg(long, conflicts_with = "cloud")]
    pub local: bool,

    /// Run through the configured Gestalt server
    #[arg(long, conflicts_with = "local")]
    pub cloud: bool,

    /// Agent harness name for local launch; defaults to the server-selected harness
    #[arg(long)]
    pub harness: Option<String>,

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

    /// Add a tool in app:operation form to each turn
    #[arg(long = "tool", value_parser = AgentToolArg::parse)]
    pub tools: Vec<AgentToolArg>,
}

#[derive(Subcommand)]
pub enum AgentCommands {
    /// Resume an interactive agent session
    Resume(AgentResumeArgs),
    /// Check the configured local agent harness
    Doctor(AgentDoctorArgs),
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
pub struct AgentDoctorArgs {
    /// Agent provider name; defaults to the configured default
    #[arg(long)]
    pub provider: Option<String>,

    /// Agent harness name; defaults to the server-selected harness
    #[arg(long)]
    pub harness: Option<String>,
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

    /// Add a tool in app:operation form to each turn
    #[arg(long = "tool", value_parser = AgentToolArg::parse)]
    pub tools: Vec<AgentToolArg>,
}

#[derive(Subcommand)]
pub enum WorkflowScheduleCommands {
    /// List workflow schedules
    List {
        /// Filter schedules by target app
        #[arg(long)]
        app: Option<String>,
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
    /// List workflow triggers
    List {
        /// Filter triggers by target app
        #[arg(long)]
        app: Option<String>,
        /// Filter triggers by event type
        #[arg(long = "type")]
        event_type: Option<String>,
    },
    /// Show a single workflow trigger
    Get {
        /// Trigger ID
        id: String,
    },
    /// Create a workflow trigger
    Create(WorkflowTriggerCreateArgs),
    /// Update an existing workflow trigger
    Update(WorkflowTriggerUpdateArgs),
    /// Delete a workflow trigger
    Delete {
        /// Trigger ID
        id: String,
    },
    /// Pause a workflow trigger
    Pause {
        /// Trigger ID
        id: String,
    },
    /// Resume a paused workflow trigger
    Resume {
        /// Trigger ID
        id: String,
    },
}

#[derive(Subcommand)]
pub enum WorkflowRunCommands {
    /// List workflow runs
    List {
        /// Filter runs by target app
        #[arg(long)]
        app: Option<String>,
        /// Filter runs by status
        #[arg(long)]
        status: Option<String>,
        /// Number of runs to request
        #[arg(long)]
        page_size: Option<u32>,
        /// Token returned by a previous list response
        #[arg(long)]
        page_token: Option<String>,
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
        /// Maximum number of summary sessions to fetch
        #[arg(long, conflicts_with = "full")]
        limit: Option<usize>,
        /// Fetch the legacy full session list without summary pagination
        #[arg(long)]
        full: bool,
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

#[derive(Args)]
pub struct WorkflowScheduleCreateArgs {
    /// Cron expression (e.g. "0 */5 * * *")
    #[arg(long)]
    pub cron: String,

    /// Target app (e.g. "slack", "github")
    #[arg(long)]
    pub app: String,

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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AgentToolArg {
    pub app: String,
    pub operation: String,
}

impl AgentToolArg {
    pub fn parse(input: &str) -> Result<Self, String> {
        let trimmed = input.trim();
        if trimmed.is_empty() {
            return Err("tool cannot be empty".to_string());
        }
        let (app, operation) = trimmed
            .split_once(':')
            .ok_or_else(|| format!("tool '{trimmed}' must use app:operation"))?;
        let app = app.trim();
        let operation = operation.trim();
        if app.is_empty() || operation.is_empty() {
            return Err(format!(
                "tool '{trimmed}' must include both app and operation"
            ));
        }
        Ok(Self {
            app: app.to_string(),
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

    /// Add a tool in app:operation form
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
pub struct WorkflowTriggerCreateArgs {
    /// Workflow provider backend to store the trigger in
    #[arg(long)]
    pub provider: Option<String>,

    /// Event type to match exactly
    #[arg(long = "type")]
    pub event_type: String,

    /// Optional event source to match exactly
    #[arg(long)]
    pub source: Option<String>,

    /// Optional event subject to match exactly
    #[arg(long)]
    pub subject: Option<String>,

    /// Target app (e.g. "slack", "github")
    #[arg(long)]
    pub app: Option<String>,

    /// Target operation (e.g. "chat.postMessage")
    #[arg(long)]
    pub operation: Option<String>,

    /// Load the full target JSON object from a file (use "-" for stdin)
    #[arg(long = "target-file")]
    pub target_file: Option<String>,

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

    /// Target app (leave unset to keep existing)
    #[arg(long)]
    pub app: Option<String>,

    /// Target operation (leave unset to keep existing)
    #[arg(long)]
    pub operation: Option<String>,

    /// Target step ID to update when the workflow has multiple steps
    #[arg(long = "step-id")]
    pub step_id: Option<String>,

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

    /// Target app (leave unset to keep existing)
    #[arg(long)]
    pub app: Option<String>,

    /// Target operation (leave unset to keep existing)
    #[arg(long)]
    pub operation: Option<String>,

    /// Target step ID to update when the workflow has multiple steps
    #[arg(long = "step-id")]
    pub step_id: Option<String>,

    /// Named connection (leave unset to keep existing; pass empty string to clear)
    #[arg(long)]
    pub connection: Option<String>,

    /// Stored connection instance (leave unset to keep existing; pass empty string to clear)
    #[arg(long)]
    pub instance: Option<String>,

    /// Mark the trigger as paused
    #[arg(long, conflicts_with = "unpaused", action = clap::ArgAction::SetTrue)]
    pub paused: bool,

    /// Mark the trigger as not paused
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
