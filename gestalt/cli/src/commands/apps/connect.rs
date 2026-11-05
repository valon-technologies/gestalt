use std::collections::{BTreeMap, BTreeSet};
use std::fmt;
use std::io::{self, IsTerminal, Read};

use anyhow::{Context, Result, anyhow, bail};
use serde::{Deserialize, Serialize};

use crate::api::{ApiClient, ApiError};
use crate::interactive::{InputPrompt, PromptOption, prompt_input, prompt_select};
use crate::output;

use super::ConnectionName;

#[derive(Debug, Clone, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
struct IntegrationInfo {
    name: String,
    #[serde(default)]
    display_name: Option<String>,
    #[serde(default)]
    description: Option<String>,
    #[serde(default)]
    connections: Vec<ConnectionDefInfo>,
    #[serde(default)]
    status: Option<String>,
    #[serde(default)]
    credential_state: Option<String>,
    #[serde(default)]
    actions: Vec<String>,
    #[serde(default)]
    source_tree_url: Option<String>,
}

#[derive(Debug, Clone, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
struct ConnectionDefInfo {
    name: String,
    #[serde(default)]
    display_name: Option<String>,
    #[serde(default)]
    auth_types: Vec<String>,
    #[serde(default)]
    connection_params: BTreeMap<String, ConnectionParamDef>,
    #[serde(default)]
    credential_fields: Vec<CredentialFieldInfo>,
    #[serde(default)]
    credential_mode: Option<String>,
    #[serde(default)]
    status: Option<String>,
    #[serde(default)]
    credential_state: Option<String>,
    #[serde(default)]
    actions: Vec<String>,
}

#[derive(Debug, Clone, Deserialize, Default)]
struct ConnectionParamDef {
    #[serde(default)]
    required: bool,
    #[serde(default)]
    description: Option<String>,
    #[serde(default)]
    default: Option<String>,
}

#[derive(Debug, Clone, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
struct ConnectManualResponse {
    status: String,
    #[serde(default)]
    integration: Option<String>,
    #[serde(default)]
    selection_url: Option<String>,
    #[serde(default)]
    pending_token: Option<String>,
    #[serde(default)]
    candidates: Vec<DiscoveryCandidateInfo>,
}

#[derive(Debug, Clone, Deserialize, Default, PartialEq, Eq)]
struct CredentialFieldInfo {
    name: String,
    #[serde(default)]
    label: Option<String>,
    #[serde(default)]
    description: Option<String>,
}

#[derive(Debug, Clone, Deserialize, Default, PartialEq, Eq)]
struct DiscoveryCandidateInfo {
    id: String,
    #[serde(default)]
    name: String,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct StartOAuthRequest<'a> {
    integration: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    connection: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    instance: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    service_account_id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    connection_params: Option<&'a BTreeMap<String, String>>,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct ConnectManualRequest<'a> {
    integration: &'a str,
    #[serde(flatten)]
    credentials: ManualCredentialRequest<'a>,
    #[serde(skip_serializing_if = "Option::is_none")]
    connection: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    instance: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    service_account_id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    connection_params: Option<&'a BTreeMap<String, String>>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum ConnectMode {
    OAuth,
    Manual,
}

#[derive(Debug, Clone)]
struct ResolvedConnection<'a> {
    wire_name: String,
    selector_name: String,
    definition: Option<&'a ConnectionDefInfo>,
}

impl<'a> ResolvedConnection<'a> {
    fn from_definition(connection: &'a ConnectionDefInfo) -> Self {
        let selector_name = ConnectionName::new(&connection.name).display().to_string();
        Self {
            wire_name: connection.name.clone(),
            selector_name,
            definition: Some(connection),
        }
    }

    fn from_requested(name: &str) -> Self {
        Self {
            wire_name: name.to_string(),
            selector_name: ConnectionName::new(name).display().to_string(),
            definition: None,
        }
    }

    fn wire_name(&self) -> &str {
        &self.wire_name
    }

    fn selector_name(&self) -> &str {
        &self.selector_name
    }
}

#[derive(Debug, Clone)]
struct ResolvedConnectFlow<'a> {
    integration: &'a IntegrationInfo,
    connection: Option<ResolvedConnection<'a>>,
    mode: ConnectMode,
    connection_params: Option<&'a BTreeMap<String, ConnectionParamDef>>,
    credential_fields: &'a [CredentialFieldInfo],
}

impl<'a> ResolvedConnectFlow<'a> {
    fn resolve(
        integration: &'a IntegrationInfo,
        requested_connection: Option<&str>,
        interactive: bool,
    ) -> Result<Self> {
        let connection = resolve_connection(integration, requested_connection, interactive)?;
        let definition = connection.as_ref().and_then(|selected| selected.definition);
        let auth_types = definition
            .map(|connection| connection.auth_types.as_slice())
            .unwrap_or(&[]);
        let connection_params = definition.map(|connection| &connection.connection_params);
        let credential_fields = definition
            .map(|connection| connection.credential_fields.as_slice())
            .unwrap_or(&[]);
        validate_user_connectable(integration, connection.as_ref())?;
        if auth_types.is_empty()
            && (integration.needs_user_connection()
                || definition.is_some_and(ConnectionDefInfo::needs_user_connection))
        {
            return Err(missing_connection_metadata_error(integration));
        }

        Ok(Self {
            integration,
            connection,
            mode: resolve_connect_mode(&integration.name, auth_types)?,
            connection_params,
            credential_fields,
        })
    }

    fn integration_name(&self) -> &str {
        &self.integration.name
    }

    fn integration_display_name(&self) -> &str {
        self.integration.display_name()
    }

    fn integration_description(&self) -> Option<&str> {
        non_empty(self.integration.description.as_deref())
    }

    fn connection_name(&self) -> Option<&str> {
        self.connection.as_ref().map(ResolvedConnection::wire_name)
    }

    fn connection_label(&self) -> Option<&str> {
        self.connection
            .as_ref()
            .map(ResolvedConnection::selector_name)
    }

    fn connection_param_defs(&self) -> Option<&BTreeMap<String, ConnectionParamDef>> {
        self.connection_params.filter(|params| !params.is_empty())
    }

    fn credential_fields(&self) -> &[CredentialFieldInfo] {
        self.credential_fields
    }
}

#[derive(Debug, Clone)]
struct ManualCredentials {
    values: BTreeMap<String, String>,
}

#[derive(Serialize)]
struct ManualCredentialRequest<'a> {
    #[serde(skip_serializing_if = "Option::is_none")]
    credential: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    credentials: Option<&'a BTreeMap<String, String>>,
}

/// Where a `--credential <field>=<value>` value comes from. Values are
/// secrets, so nothing here derives `Debug` over them.
#[derive(Clone, PartialEq, Eq)]
enum CredentialSource {
    Literal(String),
    Stdin,
}

#[derive(Clone, PartialEq, Eq)]
struct CredentialArg {
    field: String,
    source: CredentialSource,
}

impl fmt::Debug for CredentialArg {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let source = match self.source {
            CredentialSource::Literal(_) => "<redacted>",
            CredentialSource::Stdin => "-",
        };
        write!(f, "{}={}", self.field, source)
    }
}

/// Credentials supplied on the command line plus whether the CLI may prompt
/// for anything else. Prompting is disabled once the caller opts into
/// `--credential` while stdin is not a terminal, or when stdin carries a
/// credential value.
#[derive(Debug, Clone)]
struct CredentialInput {
    args: Vec<CredentialArg>,
    interactive: bool,
}

impl CredentialInput {
    fn parse(raw: &[String], stdin_is_terminal: bool) -> Result<Self> {
        let mut args: Vec<CredentialArg> = Vec::with_capacity(raw.len());
        for entry in raw {
            // Never echo the raw entry: without '=' it may be a bare secret.
            let Some((field, value)) = entry.split_once('=') else {
                bail!(
                    "--credential expects <field>=<value> (use <field>=- to read the value from stdin)"
                );
            };
            let field = field.trim();
            if field.is_empty() {
                bail!("--credential expects <field>=<value>; the field name is empty");
            }
            if args.iter().any(|arg| arg.field == field) {
                bail!("--credential field '{field}' was given more than once");
            }
            let source = match value {
                "-" => CredentialSource::Stdin,
                "" => bail!("--credential field '{field}' has an empty value"),
                value => CredentialSource::Literal(value.to_string()),
            };
            args.push(CredentialArg {
                field: field.to_string(),
                source,
            });
        }

        let stdin_fields: Vec<&str> = args
            .iter()
            .filter(|arg| arg.source == CredentialSource::Stdin)
            .map(|arg| arg.field.as_str())
            .collect();
        if stdin_fields.len() > 1 {
            bail!(
                "only one --credential may read from stdin ('-'); got: {}",
                stdin_fields.join(", ")
            );
        }

        let interactive = args.is_empty() || (stdin_fields.is_empty() && stdin_is_terminal);
        Ok(Self { args, interactive })
    }

    fn is_empty(&self) -> bool {
        self.args.is_empty()
    }

    fn stdin_field(&self) -> Option<&str> {
        self.args
            .iter()
            .find(|arg| arg.source == CredentialSource::Stdin)
            .map(|arg| arg.field.as_str())
    }

    fn non_interactive_reason(&self) -> String {
        match self.stdin_field() {
            Some(field) => format!("stdin is used by --credential {field}=-"),
            None => "stdin is not a terminal".to_string(),
        }
    }

    /// Checks supplied field names against the connection's fields and, when
    /// prompting is unavailable, that every field was supplied. Runs before any
    /// prompt or stdin read so mistakes fail fast.
    fn validate(&self, fields: &[CredentialFieldInfo]) -> Result<()> {
        let fields = effective_credential_fields(fields);
        let valid: Vec<&str> = fields.iter().map(|field| field.name.as_str()).collect();
        for arg in &self.args {
            if !valid.contains(&arg.field.as_str()) {
                bail!(
                    "unknown credential field '{}'; valid fields: {}",
                    arg.field,
                    valid.join(", ")
                );
            }
        }

        if !self.interactive {
            let supplied: BTreeSet<&str> = self.args.iter().map(|arg| arg.field.as_str()).collect();
            let missing: Vec<&str> = valid
                .iter()
                .copied()
                .filter(|name| !supplied.contains(name))
                .collect();
            if !missing.is_empty() {
                bail!(
                    "missing credential field(s): {}; pass --credential <field>=<value> for each ({}, so the CLI cannot prompt)",
                    missing.join(", "),
                    self.non_interactive_reason()
                );
            }
        }
        Ok(())
    }
}

fn read_stdin_to_string() -> Result<String> {
    let mut value = String::new();
    io::stdin()
        .read_to_string(&mut value)
        .context("failed to read credential from stdin")?;
    Ok(value)
}

fn strip_one_trailing_newline(mut value: String) -> String {
    if value.ends_with('\n') {
        value.pop();
        if value.ends_with('\r') {
            value.pop();
        }
    }
    value
}

impl ManualCredentials {
    fn collect(fields: &[CredentialFieldInfo], input: &CredentialInput) -> Result<Self> {
        Self::collect_with(fields, input, read_stdin_to_string, prompt_for_credential)
    }

    fn collect_with<R, P>(
        fields: &[CredentialFieldInfo],
        input: &CredentialInput,
        read_stdin: R,
        mut prompt: P,
    ) -> Result<Self>
    where
        R: FnOnce() -> Result<String>,
        P: FnMut(&CredentialFieldInfo) -> Result<String>,
    {
        input.validate(fields)?;

        let mut stdin_value = match input.stdin_field() {
            Some(field) => {
                let value = strip_one_trailing_newline(read_stdin()?);
                if value.is_empty() {
                    bail!("--credential {field}=- read an empty value from stdin");
                }
                Some(value)
            }
            None => None,
        };

        let fields = effective_credential_fields(fields);
        let mut values = BTreeMap::new();
        for field in &fields {
            let supplied = input.args.iter().find(|arg| arg.field == field.name);
            let value = match supplied.map(|arg| &arg.source) {
                Some(CredentialSource::Literal(value)) => value.clone(),
                Some(CredentialSource::Stdin) => stdin_value
                    .take()
                    .context("stdin credential value already consumed")?,
                None => prompt(field)?,
            };
            values.insert(field.name.clone(), value);
        }
        Ok(Self { values })
    }

    fn request(&self) -> ManualCredentialRequest<'_> {
        if self.values.len() == 1 {
            return ManualCredentialRequest {
                credential: self.values.values().next().map(String::as_str),
                credentials: None,
            };
        }

        ManualCredentialRequest {
            credential: None,
            credentials: Some(&self.values),
        }
    }
}

pub fn connect(
    client: &ApiClient,
    name: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    service_account_id: Option<&str>,
    credentials: &[String],
) -> Result<()> {
    connect_with_browser_opener(
        client,
        name,
        connection,
        instance,
        service_account_id,
        credentials,
        |url| open::that(url).map(|_| ()).map_err(Into::into),
    )
}

pub fn connect_with_browser_opener<F>(
    client: &ApiClient,
    name: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    service_account_id: Option<&str>,
    credentials: &[String],
    open_browser: F,
) -> Result<()>
where
    F: FnOnce(&str) -> Result<()>,
{
    let credentials = CredentialInput::parse(credentials, io::stdin().is_terminal())?;
    let integration = fetch_app(client, name)?;
    let flow = ResolvedConnectFlow::resolve(&integration, connection, credentials.interactive)?;

    match flow.mode {
        ConnectMode::OAuth => {
            if !credentials.is_empty() {
                bail!(
                    "--credential applies only to manual-auth connections; app '{}'{} connects through OAuth in the browser",
                    flow.integration_name(),
                    flow.connection_label()
                        .map(|label| format!(" connection '{label}'"))
                        .unwrap_or_default()
                );
            }
            start_oauth(client, &flow, instance, service_account_id, open_browser)
        }
        ConnectMode::Manual => {
            connect_manual(client, &flow, instance, service_account_id, &credentials)
        }
    }
}

fn start_oauth<F>(
    client: &ApiClient,
    flow: &ResolvedConnectFlow<'_>,
    instance: Option<&str>,
    service_account_id: Option<&str>,
    open_browser: F,
) -> Result<()>
where
    F: FnOnce(&str) -> Result<()>,
{
    let connection_params = prompt_connection_params(flow.connection_param_defs())?;
    let resp = client
        .post(
            "/api/v1/auth/start-oauth",
            &StartOAuthRequest {
                integration: flow.integration_name(),
                connection: flow.connection_name(),
                instance,
                service_account_id,
                connection_params: connection_params.as_ref(),
            },
        )
        .map_err(rewrite_connect_api_error)
        .context("failed to start OAuth flow")?;
    let url = resp["url"]
        .as_str()
        .context("response missing 'url' field")?;

    eprintln!("Opening browser to connect {}...", flow.integration_name());
    eprintln!("If the browser doesn't open, visit: {}", url);

    if open_browser(url).is_err() {
        eprintln!("Could not open browser automatically.");
    }

    Ok(())
}

fn connect_manual(
    client: &ApiClient,
    flow: &ResolvedConnectFlow<'_>,
    instance: Option<&str>,
    service_account_id: Option<&str>,
    credential_input: &CredentialInput,
) -> Result<()> {
    credential_input.validate(flow.credential_fields())?;
    eprintln!(
        "Connecting {} with manual auth.",
        flow.integration_display_name()
    );
    if let Some(description) = flow.integration_description() {
        eprintln!("{description}");
    }
    if let Some(connection_name) = flow.connection_label() {
        eprintln!("Connection: {connection_name}");
    }

    let connection_params = if credential_input.interactive {
        prompt_connection_params(flow.connection_param_defs())?
    } else {
        default_connection_params(
            flow.connection_param_defs(),
            &credential_input.non_interactive_reason(),
        )?
    };
    let credentials = ManualCredentials::collect(flow.credential_fields(), credential_input)?;
    let response: ConnectManualResponse = serde_json::from_value(
        client
            .post(
                "/api/v1/auth/connect-manual",
                &ConnectManualRequest {
                    integration: flow.integration_name(),
                    credentials: credentials.request(),
                    connection: flow.connection_name(),
                    instance,
                    service_account_id,
                    connection_params: connection_params.as_ref(),
                },
            )
            .map_err(rewrite_connect_api_error)
            .context("failed to connect app")?,
    )
    .context("failed to parse manual connect response")?;

    match response.status.as_str() {
        "connected" => {
            output::print_success(&format!(
                "Connected {}.",
                response
                    .integration
                    .as_deref()
                    .unwrap_or(flow.integration_name())
            ));
            Ok(())
        }
        "selection_required" => complete_pending_selection(
            client,
            response
                .integration
                .as_deref()
                .unwrap_or(flow.integration_name()),
            response.selection_url.as_deref(),
            response.pending_token.as_deref(),
            &response.candidates,
        ),
        other => bail!("unexpected manual connect response status '{}'", other),
    }
}

fn complete_pending_selection(
    client: &ApiClient,
    integration: &str,
    selection_url: Option<&str>,
    pending_token: Option<&str>,
    candidates: &[DiscoveryCandidateInfo],
) -> Result<()> {
    let selection_url = selection_url.context("manual connect response missing selection_url")?;
    let pending_token = pending_token.context("manual connect response missing pending_token")?;
    if candidates.is_empty() {
        bail!("manual connect response missing discovery candidates");
    }

    let selected = if candidates.len() == 1 {
        0
    } else {
        let options: Vec<PromptOption> = candidates
            .iter()
            .map(|candidate| PromptOption {
                label: candidate.display_name(),
                detail: Some(candidate.id.clone()),
            })
            .collect();
        prompt_select(
            &format!(
                "Gestalt found more than one {} connection. Choose one to save:",
                integration
            ),
            &options,
        )?
    };

    client
        .post_form(
            selection_url,
            &[
                ("pending_token", pending_token.to_string()),
                ("candidate_index", selected.to_string()),
            ],
        )
        .context("failed to finalize selected connection")?;

    output::print_success(&format!(
        "Connected {} ({})",
        integration,
        candidates[selected].display_name()
    ));
    Ok(())
}

fn fetch_app(client: &ApiClient, name: &str) -> Result<IntegrationInfo> {
    let apps: Vec<IntegrationInfo> =
        serde_json::from_value(client.get("/api/v1/apps").context("failed to load apps")?)
            .context("failed to parse apps")?;

    apps.into_iter()
        .find(|app| app.name == name)
        .with_context(|| format!("app '{}' not found", name))
}

fn resolve_connection<'a>(
    integration: &'a IntegrationInfo,
    requested_connection: Option<&str>,
    interactive: bool,
) -> Result<Option<ResolvedConnection<'a>>> {
    if integration.connections.is_empty() {
        return Ok(requested_connection.map(ResolvedConnection::from_requested));
    }

    if let Some(requested_connection) = requested_connection {
        return integration
            .connection_by_name(requested_connection)
            .map(ResolvedConnection::from_definition)
            .map(Some)
            .with_context(|| {
                format!(
                    "unknown connection '{}' for app '{}'; available connections: {}",
                    requested_connection,
                    integration.name,
                    integration.available_connections()
                )
            });
    }

    if integration.connections.len() == 1 {
        return Ok(Some(ResolvedConnection::from_definition(
            &integration.connections[0],
        )));
    }

    if !interactive {
        bail!(
            "app '{}' has multiple connections; pass --connection to choose one non-interactively; available connections: {}",
            integration.name,
            integration.available_connections()
        );
    }

    let prompt_options: Vec<PromptOption> = integration
        .connections
        .iter()
        .map(|connection| PromptOption {
            label: connection.display_name(),
            detail: Some(format!(
                "Auth: {}",
                format_auth_types(&connection.auth_types)
            )),
        })
        .collect();
    let idx = prompt_select(
        &format!("Select a {} connection:", integration.display_name()),
        &prompt_options,
    )?;

    Ok(Some(ResolvedConnection::from_definition(
        &integration.connections[idx],
    )))
}

fn resolve_connect_mode(integration: &str, auth_types: &[String]) -> Result<ConnectMode> {
    if auth_types.iter().any(|auth_type| auth_type == "oauth") {
        return Ok(ConnectMode::OAuth);
    }

    if auth_types.iter().any(|auth_type| auth_type == "manual") {
        return Ok(ConnectMode::Manual);
    }

    bail!(
        "app '{}' does not expose a supported connection flow in the CLI",
        integration
    );
}

/// Builds the diagnostic for an app the server reports as needing a user
/// connection while advertising no connection name, auth type, or credential
/// fields. The CLI cannot invent a flow for it, and the user cannot fix it
/// locally, so say who can.
fn missing_connection_metadata_error(integration: &IntegrationInfo) -> anyhow::Error {
    let mut message = format!(
        "app '{}' needs a user connection, but the server advertised no connection details \
         (connection name, auth type, credential fields), so the CLI cannot offer a connect flow.",
        integration.name
    );
    if let Some(source) = non_empty(integration.source_tree_url.as_deref()) {
        message.push_str(&format!("\nApp source: {source}"));
    }
    message.push_str(
        "\nThis cannot be fixed locally: a gestaltd admin must deploy a gestaltd that includes \
         registry-app connection metadata (gestalt#3613) or declare the app's connection in the \
         deploy config.",
    );
    anyhow!(message)
}

fn validate_user_connectable(
    integration: &IntegrationInfo,
    connection: Option<&ResolvedConnection<'_>>,
) -> Result<()> {
    if let Some(definition) = connection.and_then(|selected| selected.definition)
        && let Some("none") = definition.credential_mode.as_deref()
    {
        bail!(
            "app '{}' connection '{}' does not require a user connection",
            integration.name,
            definition.display_name()
        );
    }
    Ok(())
}

fn rewrite_connect_api_error(err: anyhow::Error) -> anyhow::Error {
    for cause in err.chain() {
        if let Some(api_error) = cause.downcast_ref::<ApiError>()
            && let Some("instance_selection_required") = api_error.code()
        {
            return anyhow::anyhow!(
                "multiple connected instances exist; pass --instance to choose one"
            );
        }
    }
    err
}

fn prompt_connection_params(
    connection_params: Option<&BTreeMap<String, ConnectionParamDef>>,
) -> Result<Option<BTreeMap<String, String>>> {
    let Some(connection_params) = connection_params else {
        return Ok(None);
    };

    let mut values = BTreeMap::new();
    for (name, def) in connection_params {
        let value = prompt_input(&InputPrompt {
            label: non_empty(def.description.as_deref())
                .unwrap_or(name)
                .to_string(),
            description: None,
            default: non_empty(def.default.as_deref()).map(str::to_string),
            required: def.required,
            secret: false,
        })?;
        if !value.is_empty() {
            values.insert(name.clone(), value);
        }
    }

    if values.is_empty() {
        Ok(None)
    } else {
        Ok(Some(values))
    }
}

/// Resolves connection parameters without prompting: defaults are used,
/// optional parameters without defaults are omitted, and required parameters
/// without defaults are an error.
fn default_connection_params(
    connection_params: Option<&BTreeMap<String, ConnectionParamDef>>,
    reason: &str,
) -> Result<Option<BTreeMap<String, String>>> {
    let Some(connection_params) = connection_params else {
        return Ok(None);
    };

    let mut values = BTreeMap::new();
    let mut missing = Vec::new();
    for (name, def) in connection_params {
        match non_empty(def.default.as_deref()) {
            Some(default) => {
                values.insert(name.clone(), default.to_string());
            }
            None if def.required => missing.push(name.as_str()),
            None => {}
        }
    }
    if !missing.is_empty() {
        bail!(
            "cannot prompt for required connection parameter(s) {} ({reason}); run `gestalt app connect` from a terminal",
            missing.join(", ")
        );
    }

    if values.is_empty() {
        Ok(None)
    } else {
        Ok(Some(values))
    }
}

fn effective_credential_fields(fields: &[CredentialFieldInfo]) -> Vec<CredentialFieldInfo> {
    if fields.is_empty() {
        vec![CredentialFieldInfo {
            name: "credential".to_string(),
            label: Some("Credential".to_string()),
            description: None,
        }]
    } else {
        fields.to_vec()
    }
}

fn prompt_for_credential(field: &CredentialFieldInfo) -> Result<String> {
    prompt_input(&InputPrompt {
        label: field.display_label(),
        description: non_empty(field.description.as_deref()).map(str::to_string),
        default: None,
        required: true,
        secret: true,
    })
}

fn non_empty(value: Option<&str>) -> Option<&str> {
    value.and_then(|value| {
        let trimmed = value.trim();
        if trimmed.is_empty() {
            None
        } else {
            Some(trimmed)
        }
    })
}

fn format_auth_types(auth_types: &[String]) -> String {
    auth_types
        .iter()
        .map(|auth_type| match auth_type.as_str() {
            "oauth" => "OAuth",
            "manual" => "manual",
            _ => auth_type.as_str(),
        })
        .collect::<Vec<_>>()
        .join(" / ")
}

fn needs_user_connection(
    status: Option<&str>,
    credential_state: Option<&str>,
    actions: &[String],
) -> bool {
    status == Some("needs_user_connection")
        || credential_state == Some("missing")
        || actions.iter().any(|action| action == "connect")
}

impl IntegrationInfo {
    fn needs_user_connection(&self) -> bool {
        needs_user_connection(
            self.status.as_deref(),
            self.credential_state.as_deref(),
            &self.actions,
        )
    }

    fn display_name(&self) -> &str {
        non_empty(self.display_name.as_deref()).unwrap_or(&self.name)
    }

    fn connection_by_name(&self, name: &str) -> Option<&ConnectionDefInfo> {
        let requested = ConnectionName::new(name);
        self.connections
            .iter()
            .find(|connection| requested.matches(&connection.name))
    }

    fn available_connections(&self) -> String {
        self.connections
            .iter()
            .map(|connection| ConnectionName::new(&connection.name).display().to_string())
            .collect::<Vec<_>>()
            .join(", ")
    }
}

impl ConnectionDefInfo {
    fn needs_user_connection(&self) -> bool {
        needs_user_connection(
            self.status.as_deref(),
            self.credential_state.as_deref(),
            &self.actions,
        )
    }

    fn display_name(&self) -> String {
        non_empty(self.display_name.as_deref())
            .unwrap_or(ConnectionName::new(&self.name).display())
            .to_string()
    }
}

impl CredentialFieldInfo {
    fn display_label(&self) -> String {
        non_empty(self.label.as_deref())
            .unwrap_or(&self.name)
            .to_string()
    }
}

impl DiscoveryCandidateInfo {
    fn display_name(&self) -> String {
        non_empty(Some(self.name.as_str()))
            .unwrap_or(&self.id)
            .to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const VCI_BRAIN_UNADVERTISED: &str = r#"{
        "name":"vci-brain","displayName":"vci-brain",
        "description":"MCP upstream: https://vci-brain.valon.tools/api/mcp",
        "managementPath":"/apps/vci-brain/admin",
        "sourceTreeUrl":"https://github.com/valon-technologies/valon-tools/tree/b6ae1dd/apps/vci-brain",
        "connections":[],"surfaces":[],"status":"needs_user_connection","credentialState":"missing",
        "healthState":"not_applicable","actions":[],"connected":false
    }"#;

    fn app(json: &str) -> IntegrationInfo {
        serde_json::from_str(json).unwrap()
    }

    fn fields(names: &[&str]) -> Vec<CredentialFieldInfo> {
        names
            .iter()
            .map(|name| CredentialFieldInfo {
                name: name.to_string(),
                label: None,
                description: None,
            })
            .collect()
    }

    fn input(raw: &[&str], stdin_is_terminal: bool) -> Result<CredentialInput> {
        let raw: Vec<String> = raw.iter().map(|entry| entry.to_string()).collect();
        CredentialInput::parse(&raw, stdin_is_terminal)
    }

    fn no_stdin() -> Result<String> {
        panic!("stdin must not be read")
    }

    fn no_prompt(field: &CredentialFieldInfo) -> Result<String> {
        panic!("field '{}' must not be prompted", field.name)
    }

    fn resolve_error(integration: &IntegrationInfo, connection: Option<&str>) -> String {
        format!(
            "{:#}",
            ResolvedConnectFlow::resolve(integration, connection, true).unwrap_err()
        )
    }

    #[test]
    fn empty_connections_needing_connection_explains_server_gap() {
        let integration = app(VCI_BRAIN_UNADVERTISED);
        for requested in [None, Some("MCP")] {
            let err = resolve_error(&integration, requested);
            assert!(
                err.starts_with("app 'vci-brain' needs a user connection, but the server advertised no connection details"),
                "{err}"
            );
            assert!(
                err.contains("App source: https://github.com/valon-technologies/valon-tools/tree/b6ae1dd/apps/vci-brain"),
                "{err}"
            );
            assert!(err.contains("gestalt#3613"), "{err}");
            assert!(err.contains("cannot be fixed locally"), "{err}");
            assert!(!err.contains("supported connection flow"), "{err}");
        }
    }

    #[test]
    fn connection_without_auth_types_needing_connection_explains_server_gap() {
        let integration = app(
            r#"{"name":"svc","connections":[{"name":"MCP","status":"needs_user_connection"}]}"#,
        );
        let err = resolve_error(&integration, None);
        assert!(err.contains("advertised no connection details"), "{err}");
        assert!(!err.contains("App source"), "{err}");
    }

    #[test]
    fn empty_connections_without_connection_need_keeps_generic_message() {
        let integration = app(r#"{"name":"svc","status":"ready","connections":[]}"#);
        for requested in [None, Some("MCP")] {
            assert_eq!(
                resolve_error(&integration, requested),
                "app 'svc' does not expose a supported connection flow in the CLI"
            );
        }
    }

    #[test]
    fn credential_mode_none_keeps_no_connection_required_message() {
        let integration = app(
            r#"{"name":"svc","status":"needs_user_connection","connections":[{"name":"app","credentialMode":"none"}]}"#,
        );
        assert_eq!(
            resolve_error(&integration, None),
            "app 'svc' connection 'app' does not require a user connection"
        );
    }

    #[test]
    fn non_interactive_requires_explicit_connection_when_ambiguous() {
        let integration = app(
            r#"{"name":"svc","connections":[{"name":"a","authTypes":["manual"]},{"name":"b","authTypes":["manual"]}]}"#,
        );
        let err = format!(
            "{:#}",
            ResolvedConnectFlow::resolve(&integration, None, false).unwrap_err()
        );
        assert!(err.contains("pass --connection"), "{err}");
        assert!(err.contains("available connections: a, b"), "{err}");
        assert!(ResolvedConnectFlow::resolve(&integration, Some("b"), false).is_ok());
    }

    #[test]
    fn parses_literal_and_stdin_credentials() {
        let parsed = input(&["token=abc=def", "org=-"], true).unwrap();
        assert_eq!(
            parsed.args,
            vec![
                CredentialArg {
                    field: "token".to_string(),
                    source: CredentialSource::Literal("abc=def".to_string()),
                },
                CredentialArg {
                    field: "org".to_string(),
                    source: CredentialSource::Stdin,
                },
            ]
        );
        assert_eq!(parsed.stdin_field(), Some("org"));
        assert!(!parsed.interactive, "stdin is consumed by '-'");
    }

    #[test]
    fn interactive_only_without_credentials_or_on_a_terminal() {
        assert!(input(&[], false).unwrap().interactive);
        assert!(input(&["token=abc"], true).unwrap().interactive);
        assert!(!input(&["token=abc"], false).unwrap().interactive);
    }

    #[test]
    fn rejects_malformed_credentials_without_echoing_values() {
        for (raw, expected) in [
            (vec!["s3cr3t"], "--credential expects <field>=<value>"),
            (vec!["=s3cr3t"], "field name is empty"),
            (
                vec!["token="],
                "--credential field 'token' has an empty value",
            ),
            (
                vec!["token=s3cr3t", "token=s3cr3t"],
                "--credential field 'token' was given more than once",
            ),
            (
                vec!["token=-", "org=-"],
                "only one --credential may read from stdin ('-'); got: token, org",
            ),
        ] {
            let err = input(&raw, true).unwrap_err().to_string();
            assert!(err.contains(expected), "{err}");
            assert!(!err.contains("s3cr3t"), "{err}");
        }
    }

    #[test]
    fn debug_redacts_literal_values() {
        let parsed = input(&["token=s3cr3t", "org=-"], true).unwrap();
        let debug = format!("{parsed:?}");
        assert!(!debug.contains("s3cr3t"), "{debug}");
        assert!(debug.contains("token=<redacted>"), "{debug}");
        assert!(debug.contains("org=-"), "{debug}");
    }

    #[test]
    fn rejects_unknown_credential_field() {
        let parsed = input(&["tokn=s3cr3t"], true).unwrap();
        let err = ManualCredentials::collect_with(
            &fields(&["token", "org"]),
            &parsed,
            no_stdin,
            no_prompt,
        )
        .unwrap_err()
        .to_string();
        assert_eq!(
            err,
            "unknown credential field 'tokn'; valid fields: token, org"
        );
    }

    #[test]
    fn unknown_field_lists_generic_field_when_connection_declares_none() {
        let parsed = input(&["token=s3cr3t"], true).unwrap();
        let err = ManualCredentials::collect_with(&[], &parsed, no_stdin, no_prompt)
            .unwrap_err()
            .to_string();
        assert_eq!(
            err,
            "unknown credential field 'token'; valid fields: credential"
        );
    }

    #[test]
    fn missing_field_without_terminal_is_an_error() {
        let parsed = input(&["token=s3cr3t"], false).unwrap();
        let err = ManualCredentials::collect_with(
            &fields(&["token", "org"]),
            &parsed,
            no_stdin,
            no_prompt,
        )
        .unwrap_err()
        .to_string();
        assert_eq!(
            err,
            "missing credential field(s): org; pass --credential <field>=<value> for each (stdin is not a terminal, so the CLI cannot prompt)"
        );
    }

    #[test]
    fn missing_field_when_stdin_holds_a_value_is_an_error_before_reading_stdin() {
        let parsed = input(&["token=-"], true).unwrap();
        let err = ManualCredentials::collect_with(
            &fields(&["token", "org"]),
            &parsed,
            no_stdin,
            no_prompt,
        )
        .unwrap_err()
        .to_string();
        assert!(err.contains("missing credential field(s): org"), "{err}");
        assert!(
            err.contains("stdin is used by --credential token=-"),
            "{err}"
        );
    }

    #[test]
    fn prompts_only_for_unsupplied_fields_on_a_terminal() {
        let parsed = input(&["token=s3cr3t"], true).unwrap();
        let mut prompted = Vec::new();
        let credentials = ManualCredentials::collect_with(
            &fields(&["token", "org"]),
            &parsed,
            no_stdin,
            |field| {
                prompted.push(field.name.clone());
                Ok("acme".to_string())
            },
        )
        .unwrap();
        assert_eq!(prompted, vec!["org".to_string()]);
        assert_eq!(credentials.values["token"], "s3cr3t");
        assert_eq!(credentials.values["org"], "acme");
    }

    #[test]
    fn stdin_value_strips_exactly_one_trailing_newline() {
        for (stdin, expected) in [
            ("s3cr3t\n", "s3cr3t"),
            ("s3cr3t\r\n", "s3cr3t"),
            ("s3cr3t", "s3cr3t"),
            ("s3cr3t\n\n", "s3cr3t\n"),
        ] {
            let parsed = input(&["token=-"], false).unwrap();
            let credentials = ManualCredentials::collect_with(
                &fields(&["token"]),
                &parsed,
                || Ok(stdin.to_string()),
                no_prompt,
            )
            .unwrap();
            assert_eq!(credentials.values["token"], expected);
        }
    }

    #[test]
    fn empty_stdin_value_is_an_error() {
        let parsed = input(&["token=-"], false).unwrap();
        let err = ManualCredentials::collect_with(
            &fields(&["token"]),
            &parsed,
            || Ok("\n".to_string()),
            no_prompt,
        )
        .unwrap_err()
        .to_string();
        assert_eq!(err, "--credential token=- read an empty value from stdin");
    }

    #[test]
    fn single_supplied_field_sends_raw_credential_string() {
        let parsed = input(&["token=-"], false).unwrap();
        let credentials = ManualCredentials::collect_with(
            &fields(&["token"]),
            &parsed,
            || Ok("s3cr3t\n".to_string()),
            no_prompt,
        )
        .unwrap();
        assert_eq!(
            serde_json::to_value(credentials.request()).unwrap(),
            serde_json::json!({"credential": "s3cr3t"})
        );
    }

    #[test]
    fn multiple_supplied_fields_send_credentials_map() {
        let parsed = input(&["token=-", "org=acme"], false).unwrap();
        let credentials = ManualCredentials::collect_with(
            &fields(&["token", "org"]),
            &parsed,
            || Ok("s3cr3t\n".to_string()),
            no_prompt,
        )
        .unwrap();
        assert_eq!(
            serde_json::to_value(credentials.request()).unwrap(),
            serde_json::json!({"credentials": {"token": "s3cr3t", "org": "acme"}})
        );
    }

    #[test]
    fn non_interactive_connection_params_use_defaults_or_fail() {
        let defs: BTreeMap<String, ConnectionParamDef> = serde_json::from_str(
            r#"{"region":{"required":true,"default":"us-east"},"tenant":{"required":false}}"#,
        )
        .unwrap();
        assert_eq!(
            default_connection_params(Some(&defs), "stdin is not a terminal").unwrap(),
            Some(BTreeMap::from([(
                "region".to_string(),
                "us-east".to_string()
            )]))
        );

        let defs: BTreeMap<String, ConnectionParamDef> =
            serde_json::from_str(r#"{"site":{"required":true}}"#).unwrap();
        let err = default_connection_params(Some(&defs), "stdin is not a terminal")
            .unwrap_err()
            .to_string();
        assert!(
            err.contains("required connection parameter(s) site (stdin is not a terminal)"),
            "{err}"
        );
    }
}
