use std::collections::BTreeMap;

use anyhow::{Context, Result, bail};
use serde::{Deserialize, Serialize};

use crate::api::ApiClient;
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
    auth_types: Vec<String>,
    #[serde(default)]
    connection_params: BTreeMap<String, ConnectionParamDef>,
    #[serde(default)]
    connections: Vec<ConnectionDefInfo>,
    #[serde(default)]
    credential_fields: Vec<CredentialFieldInfo>,
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
    credential_fields: Vec<CredentialFieldInfo>,
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
    credential_fields: &'a [CredentialFieldInfo],
}

impl<'a> ResolvedConnectFlow<'a> {
    fn resolve(
        integration: &'a IntegrationInfo,
        requested_connection: Option<&str>,
    ) -> Result<Self> {
        let connection = resolve_connection(integration, requested_connection)?;
        let definition = connection.as_ref().and_then(|selected| selected.definition);
        let auth_types = definition
            .filter(|connection| !connection.auth_types.is_empty())
            .map(|connection| connection.auth_types.as_slice())
            .unwrap_or(integration.auth_types.as_slice());
        let credential_fields = definition
            .filter(|connection| !connection.credential_fields.is_empty())
            .map(|connection| connection.credential_fields.as_slice())
            .unwrap_or(integration.credential_fields.as_slice());

        Ok(Self {
            integration,
            connection,
            mode: resolve_connect_mode(&integration.name, auth_types)?,
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

    fn connection_param_defs(&self) -> &BTreeMap<String, ConnectionParamDef> {
        &self.integration.connection_params
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

impl ManualCredentials {
    fn collect(fields: &[CredentialFieldInfo]) -> Result<Self> {
        let fields = effective_credential_fields(fields);
        let mut values = BTreeMap::new();
        for field in &fields {
            values.insert(field.name.clone(), prompt_for_credential(field)?);
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
) -> Result<()> {
    connect_with_browser_opener(client, name, connection, instance, |url| {
        open::that(url).map(|_| ()).map_err(Into::into)
    })
}

pub fn connect_with_browser_opener<F>(
    client: &ApiClient,
    name: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    open_browser: F,
) -> Result<()>
where
    F: FnOnce(&str) -> Result<()>,
{
    let integration = fetch_plugin(client, name)?;
    let flow = ResolvedConnectFlow::resolve(&integration, connection)?;

    match flow.mode {
        ConnectMode::OAuth => start_oauth(client, &flow, instance, open_browser),
        ConnectMode::Manual => connect_manual(client, &flow, instance),
    }
}

fn start_oauth<F>(
    client: &ApiClient,
    flow: &ResolvedConnectFlow<'_>,
    instance: Option<&str>,
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
                connection_params: connection_params.as_ref(),
            },
        )
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
) -> Result<()> {
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

    let connection_params = prompt_connection_params(flow.connection_param_defs())?;
    let credentials = ManualCredentials::collect(flow.credential_fields())?;
    let response: ConnectManualResponse = serde_json::from_value(
        client
            .post(
                "/api/v1/auth/connect-manual",
                &ConnectManualRequest {
                    integration: flow.integration_name(),
                    credentials: credentials.request(),
                    connection: flow.connection_name(),
                    instance,
                    connection_params: connection_params.as_ref(),
                },
            )
            .context("failed to connect plugin")?,
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

fn fetch_plugin(client: &ApiClient, name: &str) -> Result<IntegrationInfo> {
    let plugins: Vec<IntegrationInfo> = serde_json::from_value(
        client
            .get("/api/v1/integrations")
            .context("failed to load plugins")?,
    )
    .context("failed to parse plugins")?;

    plugins
        .into_iter()
        .find(|plugin| plugin.name == name)
        .with_context(|| format!("plugin '{}' not found", name))
}

fn resolve_connection<'a>(
    integration: &'a IntegrationInfo,
    requested_connection: Option<&str>,
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
                    "unknown connection '{}' for plugin '{}'; available connections: {}",
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

    let options: Vec<PromptOption> = integration
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
        &options,
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
        "plugin '{}' does not expose a supported connection flow in the CLI",
        integration
    );
}

fn prompt_connection_params(
    connection_params: &BTreeMap<String, ConnectionParamDef>,
) -> Result<Option<BTreeMap<String, String>>> {
    if connection_params.is_empty() {
        return Ok(None);
    }

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

impl IntegrationInfo {
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
            .map(ConnectionDefInfo::display_name)
            .collect::<Vec<_>>()
            .join(", ")
    }
}

impl ConnectionDefInfo {
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
