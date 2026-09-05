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
    /// Run an interactive agent session or inspect agent resources
    Agent(AgentArgs),

    /// Manage apps
    #[command(alias = "apps")]
    App {
        #[command(subcommand)]
        command: AppCommands,
    },

    /// Manage authentication (login, logout)
    Auth {
        #[command(subcommand)]
        command: AuthCommands,
    },

    /// Inspect and manage authorization state
    Authorization {
        #[command(subcommand)]
        command: AuthorizationCommands,
    },

    /// Manage persistent configuration
    Config {
        #[command(subcommand)]
        command: ConfigCommands,
    },

    #[command(hide = true)]
    /// Describe an app operation
    Describe(DescribeArgs),

    /// Interactive setup wizard
    Init,

    #[command(hide = true)]
    /// Execute an app operation
    Invoke(InvokeArgs),

    /// Manage workflow resources
    #[command(alias = "workflows")]
    Workflow {
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
    /// Manage API tokens
    Token {
        #[command(subcommand)]
        command: TokenCommands,
    },
}

#[derive(Subcommand)]
pub enum ConfigCommands {
    /// Get a config value
    Get {
        /// Config key
        key: String,
    },
    /// List all config values
    List,
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
}

#[derive(Subcommand)]
pub enum AppCommands {
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

        /// Store the connection credential under this service account subject
        #[arg(long = "service-account-id")]
        service_account_id: Option<String>,
    },
    /// Describe an app operation
    Describe(DescribeArgs),
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
    /// List available apps
    List,
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

    /// Outbound header override as key=value (repeatable)
    #[arg(long = "header", value_parser = params::parse_header_entry)]
    pub headers: Vec<params::HeaderEntry>,

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
        /// OAuth scopes for the token (e.g. my-app or my-app:operation)
        #[arg(long, default_value = "")]
        scopes: String,
        /// Token lifetime in seconds (default: 30 days)
        #[arg(long = "expires-in", default_value_t = 30 * 24 * 3600)]
        expires_in: i64,
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
    /// Check whether a subject can perform an action on a resource
    CheckAccess(AuthorizationCheckAccessArgs),
    /// Inspect and manage authorization relationships
    Relationships {
        #[command(subcommand)]
        command: AuthorizationRelationshipCommands,
    },
    /// Inspect authorization models
    Models {
        #[command(subcommand)]
        command: AuthorizationModelCommands,
    },
    /// Manage authorization state snapshots
    State {
        #[command(subcommand)]
        command: AuthorizationStateCommands,
    },
    /// Manage service-account subjects
    Subjects {
        #[command(subcommand)]
        command: AuthorizationSubjectCommands,
    },
    /// Inspect and manage app-scoped authorization grants
    Apps {
        #[command(subcommand)]
        command: AuthorizationAppsCommands,
    },
}

#[derive(Subcommand)]
pub enum AuthorizationAppsCommands {
    /// List apps in the catalog
    List,
    /// Manage member grants for an app
    Members {
        #[command(subcommand)]
        command: AuthorizationAppsMembersCommands,
    },
    /// Manage allowed operation exposure for an app
    #[command(name = "allowed-operations")]
    AllowedOperations {
        #[command(subcommand)]
        command: AuthorizationAppsAllowedOperationsCommands,
    },
}

#[derive(Subcommand)]
pub enum AuthorizationAppsMembersCommands {
    /// List member grants for an app
    List(AuthorizationAppsMembersListArgs),
    /// Grant or change a member role on an app
    Set(AuthorizationAppsMembersSetArgs),
    /// Remove a member grant from an app
    Remove(AuthorizationAppsMembersRemoveArgs),
}

#[derive(Args)]
pub struct AuthorizationAppsMembersListArgs {
    /// App name
    pub app: String,
}

#[derive(Args)]
pub struct AuthorizationAppsMembersSetArgs {
    /// App name
    pub app: String,
    /// Existing roster member email address
    #[arg(long, conflicts_with = "subject_id")]
    pub email: Option<String>,
    /// Member subject id, such as user:abc or service_account:bot
    #[arg(long = "subject-id", conflicts_with = "email")]
    pub subject_id: Option<String>,
    /// App role to grant, such as viewer, editor, or admin
    #[arg(long)]
    pub role: String,
}

#[derive(Args)]
pub struct AuthorizationAppsMembersRemoveArgs {
    /// App name
    pub app: String,
    /// Member subject id, such as user:abc
    #[arg(conflicts_with_all = ["email", "subject_id"], required_unless_present_any = ["email", "subject_id"])]
    pub subject: Option<String>,
    /// Existing roster member email address
    #[arg(long, conflicts_with_all = ["subject", "subject_id"])]
    pub email: Option<String>,
    /// Member subject id, such as user:abc or service_account:bot
    #[arg(long = "subject-id", conflicts_with_all = ["subject", "email"])]
    pub subject_id: Option<String>,
    /// Relationship role to remove; when omitted, removes all mutable grants for the subject
    #[arg(long)]
    pub role: Option<String>,
}

#[derive(Subcommand)]
pub enum AuthorizationAppsAllowedOperationsCommands {
    /// List allowed operation exposure for an app
    List(AuthorizationAppsAllowedOperationsListArgs),
    /// Update allowed operation exposure for an app
    Set(AuthorizationAppsAllowedOperationsSetArgs),
}

#[derive(Args)]
pub struct AuthorizationAppsAllowedOperationsListArgs {
    /// App name
    pub app: String,
}

#[derive(Args)]
pub struct AuthorizationAppsAllowedOperationsSetArgs {
    /// App name
    pub app: String,
    /// JSON file with {"operations": {...}, "removed": [...]} in app-admin shape
    #[arg(
        long = "input-file",
        conflicts_with_all = ["set", "remove"]
    )]
    pub input_file: Option<String>,
    /// Operation override as id=viewer,editor (repeatable)
    #[arg(long, action = clap::ArgAction::Append, conflicts_with = "input_file")]
    pub set: Vec<String>,
    /// Operation id to remove from the runtime overlay (repeatable)
    #[arg(long, action = clap::ArgAction::Append, conflicts_with = "input_file")]
    pub remove: Vec<String>,
}

#[derive(Subcommand)]
pub enum AuthorizationStateCommands {
    /// Idempotently apply a model and relationship snapshot
    Apply(AuthorizationStateApplyArgs),
}

#[derive(Args)]
pub struct AuthorizationStateApplyArgs {
    /// JSON file containing model and relationships in SetAuthorizationStateRequest shape
    #[arg(long = "input-file")]
    pub input_file: String,
}

#[derive(Args)]
pub struct AuthorizationCheckAccessArgs {
    /// Subject identifier within the subject type
    #[arg(long = "subject-id")]
    pub subject_id: String,
    /// Subject type/namespace, usually subject
    #[arg(long = "subject-type")]
    pub subject_type: String,
    /// Action name to check
    #[arg(long)]
    pub action: String,
    /// Resource identifier within the resource type
    #[arg(long = "resource-id")]
    pub resource_id: String,
    /// Resource type on the relationship tuple
    #[arg(long = "resource-type")]
    pub resource_type: String,
}

#[derive(Subcommand)]
pub enum AuthorizationRelationshipCommands {
    /// List relationships
    List(AuthorizationRelationshipListArgs),
    /// Add a relationship tuple
    Add(AuthorizationRelationshipMutationArgs),
    /// Delete a relationship tuple
    Delete(AuthorizationRelationshipMutationArgs),
}

#[derive(Args)]
pub struct AuthorizationRelationshipMutationArgs {
    /// Resource type on the relationship tuple
    #[arg(long = "resource-type")]
    pub resource_type: String,
    /// Resource id on the relationship tuple
    #[arg(long = "resource-id")]
    pub resource_id: String,
    /// Relationship relation, such as admin or viewer
    #[arg(long)]
    pub relation: String,
    /// Direct subject id target, such as user:abc
    #[arg(long = "subject-id", conflicts_with_all = ["subject_set"])]
    pub subject_id: Option<String>,
    /// Subject set target, such as group:valon-employees#member
    #[arg(long = "subject-set", conflicts_with = "subject_id")]
    pub subject_set: Option<String>,
}

#[derive(Args)]
pub struct AuthorizationRelationshipListArgs {
    /// Filter by target subject id
    #[arg(long = "subject-id", requires = "subject_type")]
    pub subject_id: Option<String>,
    /// Filter by target subject type
    #[arg(long = "subject-type", requires = "subject_id")]
    pub subject_type: Option<String>,
    /// Filter to relationships with this relation name
    #[arg(long)]
    pub relation: Option<String>,
    /// Resource type on the relationship tuple
    #[arg(long = "resource-type", requires = "resource_id")]
    pub resource_type: Option<String>,
    /// Filter to relationships whose resource has this id
    #[arg(long = "resource-id", requires = "resource_type")]
    pub resource_id: Option<String>,
    /// Filter by source layer, such as static_config or runtime
    #[arg(long = "source-layer")]
    pub source_layer: Option<String>,
    /// Maximum number of relationships to return
    #[arg(long = "page-size")]
    pub page_size: Option<u32>,
    /// Pagination cursor returned by a previous list response
    #[arg(long = "page-token")]
    pub page_token: Option<String>,
}

#[derive(Subcommand)]
pub enum AuthorizationSubjectCommands {
    /// Create a managed service-account subject
    Create(AuthorizationSubjectCreateArgs),
    /// Manage grants for a service-account subject
    Grants {
        #[command(subcommand)]
        command: AuthorizationSubjectGrantCommands,
    },
    /// Manage bearer tokens for a service-account subject
    Tokens {
        #[command(subcommand)]
        command: AuthorizationSubjectTokenCommands,
    },
}

#[derive(Args)]
pub struct AuthorizationSubjectCreateArgs {
    /// Service-account id without the service_account: prefix
    pub id: String,
    /// Human-readable display name
    #[arg(long = "display-name")]
    pub display_name: String,
    /// Optional description
    #[arg(long)]
    pub description: Option<String>,
}

#[derive(Subcommand)]
pub enum AuthorizationSubjectGrantCommands {
    /// Create an authorization relationship for a service-account subject
    Set(AuthorizationSubjectGrantSetArgs),
}

#[derive(Args)]
pub struct AuthorizationSubjectGrantSetArgs {
    /// Service-account id or canonical service_account:<id> subject id
    pub subject_id: String,
    /// Relationship relation, such as admin or viewer
    #[arg(long)]
    pub relation: String,
    /// Resource type on the relationship tuple
    #[arg(long = "resource-type")]
    pub resource_type: String,
    /// Resource id on the relationship tuple
    #[arg(long = "resource-id")]
    pub resource_id: String,
}

#[derive(Subcommand)]
pub enum AuthorizationSubjectTokenCommands {
    /// Mint a bearer token owned by the service-account subject
    Create(AuthorizationSubjectTokenCreateArgs),
}

#[derive(Args)]
pub struct AuthorizationSubjectTokenCreateArgs {
    /// Service-account id or canonical service_account:<id> subject id
    pub subject_id: String,
    /// Human-readable grant label
    #[arg(long)]
    pub name: String,
    /// Permission scope entries such as github:list
    #[arg(long = "permission", required = true)]
    pub permission: Vec<String>,
    /// Token lifetime in seconds
    #[arg(long = "expires-in")]
    pub expires_in: Option<i64>,
}

#[derive(Subcommand)]
pub enum AuthorizationModelCommands {
    /// Inspect the active authorization model
    Active {
        #[command(subcommand)]
        command: AuthorizationActiveModelCommands,
    },
}

#[derive(Subcommand)]
pub enum AuthorizationActiveModelCommands {
    /// Get the active model reference
    Get,
    /// List resource types in the active model
    ResourceTypes {
        #[command(subcommand)]
        command: AuthorizationActiveModelResourceTypeCommands,
    },
}

#[derive(Subcommand)]
pub enum AuthorizationActiveModelResourceTypeCommands {
    /// List active model resource types
    List(AuthorizationActiveModelResourceTypeListArgs),
}

#[derive(Args)]
pub struct AuthorizationActiveModelResourceTypeListArgs {
    /// Filter to one resource type name
    #[arg(long)]
    pub name: Option<String>,
    /// Filter by source layer, such as static_config or runtime
    #[arg(long = "source-layer")]
    pub source_layer: Option<String>,
    /// Maximum number of resource types to return
    #[arg(long = "page-size")]
    pub page_size: Option<u32>,
    /// Pagination cursor returned by a previous list response
    #[arg(long = "page-token")]
    pub page_token: Option<String>,
}
#[derive(Subcommand)]
pub enum WorkflowCommands {
    /// Deliver workflow events
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

    /// Optional execution budget in seconds for each created turn
    #[arg(long = "timeout-seconds", value_parser = clap::value_parser!(i32).range(1..))]
    pub timeout_seconds: Option<i32>,
}

#[derive(Subcommand)]
pub enum AgentCommands {
    /// Resume an interactive agent session
    Resume(AgentResumeArgs),
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

    /// Optional execution budget in seconds for each created turn
    #[arg(long = "timeout-seconds", value_parser = clap::value_parser!(i32).range(1..))]
    pub timeout_seconds: Option<i32>,
}

#[derive(Subcommand)]
pub enum WorkflowRunCommands {
    /// Cancel a workflow run
    Cancel {
        /// Run ID
        id: String,
        /// Optional cancellation reason
        #[arg(long)]
        reason: Option<String>,
    },
    /// Show a single workflow run
    Get {
        /// Run ID
        id: String,
    },
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
}

#[derive(Subcommand)]
pub enum AgentSessionCommands {
    /// Create an agent session
    Create(AgentSessionCreateArgs),
    /// Show a single agent session
    Get {
        /// Session ID
        id: String,
    },
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
    /// Update an existing agent session
    Update(AgentSessionUpdateArgs),
}

#[derive(Subcommand)]
pub enum AgentTurnCommands {
    /// Cancel an agent turn
    Cancel {
        /// Turn ID
        id: String,
        /// Optional cancellation reason
        #[arg(long)]
        reason: Option<String>,
    },
    /// Create an agent turn within a session
    Create(AgentTurnCreateArgs),
    /// Inspect or stream agent turn events
    Events {
        #[command(subcommand)]
        command: AgentTurnEventCommands,
    },
    /// Show a single agent turn
    Get {
        /// Turn ID
        id: String,
    },
    /// List turns in a session
    List {
        /// Session ID
        session_id: String,
        /// Filter turns by status
        #[arg(long)]
        status: Option<String>,
    },
    /// Render a stored turn as a transcript
    Transcript {
        /// Turn ID
        id: String,
    },
}

#[derive(Subcommand)]
pub enum AgentTurnEventCommands {
    /// List stored events for an agent turn
    List(AgentTurnEventListArgs),
}

#[derive(Subcommand)]
pub enum WorkflowEventCommands {
    /// Deliver a workflow event
    Deliver(WorkflowEventDeliverArgs),
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

    /// Add a tool in app:operation form to the session
    #[arg(long = "tool", value_parser = AgentToolArg::parse)]
    pub tools: Vec<AgentToolArg>,

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

    /// Optional execution budget in seconds for the turn
    #[arg(long = "timeout-seconds", value_parser = clap::value_parser!(i32).range(1..))]
    pub timeout_seconds: Option<i32>,

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
pub struct WorkflowEventDeliverArgs {
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
