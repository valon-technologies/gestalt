use std::collections::BTreeMap;

use anyhow::{Context, Result, bail};

use crate::api::{ApiClient, CredentialFieldInfo, DiscoveryCandidateInfo, IntegrationInfo};
use crate::interactive::{InputPrompt, PromptOption, prompt_input, prompt_select};
use crate::output::{self, Format};

const PLUGIN_CONNECTION_NAME: &str = "_plugin";
const PLUGIN_CONNECTION_ALIAS: &str = "plugin";

#[derive(Debug, Clone)]
struct ResolvedAuthTarget {
    auth_types: Vec<String>,
    credential_fields: Vec<CredentialFieldInfo>,
}

impl ResolvedAuthTarget {
    fn supports_oauth(&self) -> bool {
        self.auth_types.iter().any(|auth_type| auth_type == "oauth")
    }

    fn supports_manual(&self) -> bool {
        self.auth_types
            .iter()
            .any(|auth_type| auth_type == "manual")
    }
}

enum ManualCredentialPayload {
    Single(String),
    Multiple(BTreeMap<String, String>),
}

pub fn list(client: &ApiClient, format: Format) -> Result<()> {
    let resp = client
        .get("/api/v1/integrations")
        .context("failed to list integrations")?;

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            let rows: Vec<Vec<String>> = resp
                .as_array()
                .unwrap_or(&Vec::new())
                .iter()
                .map(|item| {
                    let connected = match item["connected"].as_bool() {
                        Some(true) => "yes",
                        _ => "no",
                    };
                    vec![
                        item["name"].as_str().unwrap_or("-").to_string(),
                        item["description"].as_str().unwrap_or("-").to_string(),
                        connected.into(),
                    ]
                })
                .collect();
            output::print_table(&["Name", "Description", "Connected"], &rows);
        }
    }
    Ok(())
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
    let integration = fetch_integration(client, name)?;
    let selected_connection = resolve_connection(&integration, connection)?;
    let auth_target = resolve_auth_target(&integration, selected_connection.as_deref());

    if auth_target.supports_oauth() {
        return start_oauth(
            client,
            name,
            selected_connection.as_deref(),
            instance,
            open_browser,
        );
    }

    if auth_target.supports_manual() {
        return connect_manual(
            client,
            &integration,
            name,
            selected_connection.as_deref(),
            instance,
            &auth_target,
        );
    }

    bail!(
        "integration '{}' does not expose a supported connection flow in the CLI",
        name
    );
}

fn start_oauth<F>(
    client: &ApiClient,
    name: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    open_browser: F,
) -> Result<()>
where
    F: FnOnce(&str) -> Result<()>,
{
    let resp = client
        .start_integration_oauth(name, connection, instance)
        .context("failed to start OAuth flow")?;

    eprintln!("Opening browser to connect {}...", name);
    eprintln!("If the browser doesn't open, visit: {}", resp.url);

    if open_browser(&resp.url).is_err() {
        eprintln!("Could not open browser automatically.");
    }

    Ok(())
}

fn connect_manual(
    client: &ApiClient,
    integration: &IntegrationInfo,
    name: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    auth_target: &ResolvedAuthTarget,
) -> Result<()> {
    eprintln!(
        "Connecting {} with manual auth.",
        integration.display_name()
    );
    if let Some(description) = non_empty(integration.description.as_deref()) {
        eprintln!("{description}");
    }
    if let Some(connection_name) = connection {
        eprintln!(
            "Connection: {}",
            user_facing_connection_name(connection_name)
        );
    }

    let connection_params = prompt_connection_params(integration)?;
    let credential_payload = prompt_manual_credentials(&auth_target.credential_fields)?;
    let (credential, credentials) = match &credential_payload {
        ManualCredentialPayload::Single(value) => (Some(value.as_str()), None),
        ManualCredentialPayload::Multiple(values) => (None, Some(values)),
    };

    let response = client
        .connect_manual_integration(
            name,
            credential,
            credentials,
            connection_params.as_ref(),
            connection,
            instance,
        )
        .context("failed to connect integration")?;

    match response.status.as_str() {
        "connected" => {
            output::print_success(&format!(
                "Connected {}.",
                response.integration.as_deref().unwrap_or(name)
            ));
            Ok(())
        }
        "selection_required" => complete_pending_selection(
            client,
            response.integration.as_deref().unwrap_or(name),
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
        .finalize_pending_connection(selection_url, pending_token, selected)
        .context("failed to finalize selected connection")?;

    output::print_success(&format!(
        "Connected {} ({})",
        integration,
        candidates[selected].display_name()
    ));
    Ok(())
}

fn fetch_integration(client: &ApiClient, name: &str) -> Result<IntegrationInfo> {
    client
        .list_integrations_typed()
        .context("failed to load integrations")?
        .into_iter()
        .find(|integration| integration.name == name)
        .with_context(|| format!("integration '{}' not found", name))
}

fn resolve_connection(
    integration: &IntegrationInfo,
    requested_connection: Option<&str>,
) -> Result<Option<String>> {
    if integration.connections.is_empty() {
        return Ok(requested_connection.map(str::to_string));
    }

    if let Some(requested_connection) = requested_connection {
        return integration
            .connection_by_name(requested_connection)
            .map(|connection| Some(connection.name.clone()))
            .with_context(|| {
                format!(
                    "unknown connection '{}' for integration '{}'; available connections: {}",
                    requested_connection,
                    integration.name,
                    integration.available_connections()
                )
            });
    }

    if integration.connections.len() == 1 {
        return Ok(Some(integration.connections[0].name.clone()));
    }

    let options: Vec<PromptOption> = integration
        .connections
        .iter()
        .map(|connection| PromptOption {
            label: user_facing_connection_name(&connection.name).to_string(),
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
    Ok(Some(integration.connections[idx].name.clone()))
}

fn resolve_auth_target(
    integration: &IntegrationInfo,
    connection: Option<&str>,
) -> ResolvedAuthTarget {
    if let Some(connection) = connection
        && let Some(connection_info) = integration.connection_by_name(connection)
    {
        return ResolvedAuthTarget {
            auth_types: connection_info.auth_types.clone(),
            credential_fields: if connection_info.credential_fields.is_empty() {
                integration.credential_fields.clone()
            } else {
                connection_info.credential_fields.clone()
            },
        };
    }

    ResolvedAuthTarget {
        auth_types: integration.auth_types.clone(),
        credential_fields: integration.credential_fields.clone(),
    }
}

fn prompt_connection_params(
    integration: &IntegrationInfo,
) -> Result<Option<BTreeMap<String, String>>> {
    if integration.connection_params.is_empty() {
        return Ok(None);
    }

    let mut values = BTreeMap::new();
    for (name, def) in &integration.connection_params {
        let value = prompt_input(&InputPrompt {
            label: non_empty(def.description.as_deref())
                .unwrap_or(name)
                .to_string(),
            description: None,
            help_url: None,
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

fn prompt_manual_credentials(fields: &[CredentialFieldInfo]) -> Result<ManualCredentialPayload> {
    let fields = if fields.is_empty() {
        vec![CredentialFieldInfo {
            name: "credential".to_string(),
            label: Some("Credential".to_string()),
            description: None,
            help_url: None,
        }]
    } else {
        fields.to_vec()
    };

    if fields.len() == 1 {
        return Ok(ManualCredentialPayload::Single(prompt_for_credential(
            &fields[0],
        )?));
    }

    let mut values = BTreeMap::new();
    for field in &fields {
        values.insert(field.name.clone(), prompt_for_credential(field)?);
    }
    Ok(ManualCredentialPayload::Multiple(values))
}

fn prompt_for_credential(field: &CredentialFieldInfo) -> Result<String> {
    prompt_input(&InputPrompt {
        label: field.display_label(),
        description: non_empty(field.description.as_deref()).map(str::to_string),
        help_url: non_empty(field.help_url.as_deref()).map(str::to_string),
        default: None,
        required: true,
        secret: true,
    })
}

fn normalize_connection_name(name: &str) -> &str {
    if name == PLUGIN_CONNECTION_ALIAS {
        PLUGIN_CONNECTION_NAME
    } else {
        name
    }
}

fn user_facing_connection_name(name: &str) -> &str {
    if name == PLUGIN_CONNECTION_NAME {
        PLUGIN_CONNECTION_ALIAS
    } else {
        name
    }
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

    fn connection_by_name(&self, name: &str) -> Option<&crate::api::ConnectionDefInfo> {
        let normalized = normalize_connection_name(name);
        self.connections
            .iter()
            .find(|connection| normalize_connection_name(&connection.name) == normalized)
    }

    fn available_connections(&self) -> String {
        self.connections
            .iter()
            .map(|connection| user_facing_connection_name(&connection.name).to_string())
            .collect::<Vec<_>>()
            .join(", ")
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
