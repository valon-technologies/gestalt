use std::collections::BTreeMap;
use std::io::{self, IsTerminal, Write};

use anyhow::{Context, Result, bail};
use serde::{Deserialize, Serialize};

use crate::api::ApiClient;
use crate::output::{self, Format};

const PLUGIN_CONNECTION_NAME: &str = "_plugin";
const PLUGIN_CONNECTION_ALIAS: &str = "plugin";

#[derive(Serialize)]
struct StartOAuthRequest<'a> {
    integration: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    connection: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    instance: Option<&'a str>,
}

#[derive(Serialize)]
struct ConnectManualRequest<'a> {
    integration: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    credential: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    credentials: Option<&'a BTreeMap<String, String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    connection: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    instance: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    connection_params: Option<&'a BTreeMap<String, String>>,
}

#[derive(Debug, Clone, Deserialize, Default)]
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
struct ConnectionDefInfo {
    name: String,
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

#[derive(Debug, Clone, Deserialize, Default, PartialEq, Eq)]
struct CredentialFieldInfo {
    name: String,
    #[serde(default)]
    label: Option<String>,
    #[serde(default)]
    description: Option<String>,
    #[serde(default)]
    help_url: Option<String>,
}

#[derive(Debug, Clone, Deserialize, Default, PartialEq, Eq)]
struct DiscoveryCandidateInfo {
    id: String,
    #[serde(default)]
    name: String,
}

#[derive(Debug, Clone, Deserialize, Default)]
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

#[derive(Debug, Clone, PartialEq, Eq)]
struct PromptOption {
    label: String,
    detail: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct PromptInput {
    label: String,
    description: Option<String>,
    help_url: Option<String>,
    default: Option<String>,
    required: bool,
    secret: bool,
}

trait ConnectPrompter {
    fn select(&mut self, prompt: &str, options: &[PromptOption]) -> Result<usize>;
    fn input(&mut self, prompt: &PromptInput) -> Result<String>;
}

struct StdioPrompter;

impl StdioPrompter {
    fn ensure_interactive(&self) -> Result<()> {
        if !io::stdin().is_terminal() || !io::stderr().is_terminal() {
            bail!(
                "manual connections require an interactive terminal; rerun this command in a terminal session",
            );
        }
        Ok(())
    }

    fn read_line(&self) -> Result<String> {
        let mut input = String::new();
        io::stdin()
            .read_line(&mut input)
            .context("failed to read terminal input")?;
        Ok(input.trim().to_string())
    }
}

impl ConnectPrompter for StdioPrompter {
    fn select(&mut self, prompt: &str, options: &[PromptOption]) -> Result<usize> {
        self.ensure_interactive()?;
        if options.is_empty() {
            bail!("no options available");
        }

        let mut stderr = io::stderr();
        writeln!(stderr, "{prompt}")?;
        for (idx, option) in options.iter().enumerate() {
            writeln!(stderr, "  {}. {}", idx + 1, option.label)?;
            if let Some(detail) = option.detail.as_deref() {
                writeln!(stderr, "     {detail}")?;
            }
        }

        loop {
            write!(stderr, "Selection [1-{}]: ", options.len())?;
            stderr.flush()?;
            let input = self.read_line()?;

            if let Ok(choice) = input.parse::<usize>()
                && (1..=options.len()).contains(&choice)
            {
                return Ok(choice - 1);
            }

            writeln!(stderr, "Enter a number between 1 and {}.", options.len())?;
        }
    }

    fn input(&mut self, prompt: &PromptInput) -> Result<String> {
        self.ensure_interactive()?;

        let mut stderr = io::stderr();
        writeln!(stderr)?;
        writeln!(stderr, "{}", prompt.label)?;
        if let Some(description) = prompt.description.as_deref() {
            writeln!(stderr, "  {description}")?;
        }
        if let Some(help_url) = prompt.help_url.as_deref() {
            writeln!(stderr, "  Help: {help_url}")?;
        }

        loop {
            let value = if prompt.secret {
                let prompt_text = match prompt.default.as_deref() {
                    Some(default) => format!("Value [{default}]: "),
                    None => "Value: ".to_string(),
                };
                rpassword::prompt_password(prompt_text).context("failed to read secret input")?
            } else {
                match prompt.default.as_deref() {
                    Some(default) => write!(stderr, "Value [{default}]: ")?,
                    None => write!(stderr, "Value: ")?,
                }
                stderr.flush()?;
                self.read_line()?
            };

            let trimmed = value.trim().to_string();
            if trimmed.is_empty() {
                if let Some(default) = prompt.default.clone() {
                    return Ok(default);
                }
                if !prompt.required {
                    return Ok(String::new());
                }
                writeln!(stderr, "A value is required.")?;
                continue;
            }

            return Ok(trimmed);
        }
    }
}

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
    let mut prompts = StdioPrompter;
    connect_with_browser_opener_and_prompter(
        client,
        name,
        connection,
        instance,
        &mut prompts,
        |url| open::that(url).map(|_| ()).map_err(Into::into),
    )
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
    let mut prompts = StdioPrompter;
    connect_with_browser_opener_and_prompter(
        client,
        name,
        connection,
        instance,
        &mut prompts,
        open_browser,
    )
}

fn connect_with_browser_opener_and_prompter<P, F>(
    client: &ApiClient,
    name: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    prompts: &mut P,
    open_browser: F,
) -> Result<()>
where
    P: ConnectPrompter,
    F: FnOnce(&str) -> Result<()>,
{
    let integration = fetch_integration(client, name)?;
    let selected_connection = resolve_connection(&integration, connection, prompts)?;
    let auth_target = resolve_auth_target(&integration, selected_connection.as_deref())?;

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
            prompts,
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
        .post(
            "/api/v1/auth/start-oauth",
            &StartOAuthRequest {
                integration: name,
                connection,
                instance,
            },
        )
        .context("failed to start OAuth flow")?;

    let url = resp["url"]
        .as_str()
        .context("response missing 'url' field")?;

    eprintln!("Opening browser to connect {}...", name);
    eprintln!("If the browser doesn't open, visit: {}", url);

    if open_browser(url).is_err() {
        eprintln!("Could not open browser automatically.");
    }

    Ok(())
}

fn connect_manual<P>(
    client: &ApiClient,
    integration: &IntegrationInfo,
    name: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    auth_target: &ResolvedAuthTarget,
    prompts: &mut P,
) -> Result<()>
where
    P: ConnectPrompter,
{
    let display_name = integration.display_name();
    eprintln!("Connecting {display_name} with manual auth.");
    if let Some(description) = non_empty(integration.description.as_deref()) {
        eprintln!("{description}");
    }
    if let Some(connection_name) = connection {
        eprintln!(
            "Connection: {}",
            user_facing_connection_name(connection_name)
        );
    }

    let connection_params = prompt_connection_params(&integration.connection_params, prompts)?;
    let credential_payload = prompt_manual_credentials(&auth_target.credential_fields, prompts)?;

    let (credential, credentials) = match &credential_payload {
        ManualCredentialPayload::Single(value) => (Some(value.as_str()), None),
        ManualCredentialPayload::Multiple(values) => (None, Some(values)),
    };

    let resp = client
        .post(
            "/api/v1/auth/connect-manual",
            &ConnectManualRequest {
                integration: name,
                credential,
                credentials,
                connection,
                instance,
                connection_params: connection_params.as_ref(),
            },
        )
        .context("failed to connect integration")?;

    let response: ConnectManualResponse =
        serde_json::from_value(resp).context("failed to parse manual connect response")?;

    match response.status.as_str() {
        "connected" => {
            let connected_name = response.integration.as_deref().unwrap_or(name);
            output::print_success(&format!("Connected {}.", connected_name));
            Ok(())
        }
        "selection_required" => complete_pending_selection(
            client,
            response.integration.as_deref().unwrap_or(name),
            response.selection_url.as_deref(),
            response.pending_token.as_deref(),
            &response.candidates,
            prompts,
        ),
        other => bail!("unexpected manual connect response status '{}'", other),
    }
}

fn complete_pending_selection<P>(
    client: &ApiClient,
    integration: &str,
    selection_url: Option<&str>,
    pending_token: Option<&str>,
    candidates: &[DiscoveryCandidateInfo],
    prompts: &mut P,
) -> Result<()>
where
    P: ConnectPrompter,
{
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
                detail: non_empty(Some(candidate.id.as_str())).map(str::to_string),
            })
            .collect();
        prompts.select(
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

fn fetch_integration(client: &ApiClient, name: &str) -> Result<IntegrationInfo> {
    let resp = client
        .get("/api/v1/integrations")
        .context("failed to load integrations")?;
    let integrations: Vec<IntegrationInfo> =
        serde_json::from_value(resp).context("failed to parse integrations")?;

    integrations
        .into_iter()
        .find(|integration| integration.name == name)
        .with_context(|| format!("integration '{}' not found", name))
}

fn resolve_connection<P>(
    integration: &IntegrationInfo,
    requested_connection: Option<&str>,
    prompts: &mut P,
) -> Result<Option<String>>
where
    P: ConnectPrompter,
{
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
    let idx = prompts.select(
        &format!("Select a {} connection:", integration.display_name()),
        &options,
    )?;
    Ok(Some(integration.connections[idx].name.clone()))
}

fn resolve_auth_target(
    integration: &IntegrationInfo,
    connection: Option<&str>,
) -> Result<ResolvedAuthTarget> {
    if let Some(connection) = connection {
        if let Some(connection_info) = integration.connection_by_name(connection) {
            let credential_fields = if connection_info.credential_fields.is_empty() {
                integration.credential_fields.clone()
            } else {
                connection_info.credential_fields.clone()
            };
            return Ok(ResolvedAuthTarget {
                auth_types: connection_info.auth_types.clone(),
                credential_fields,
            });
        }
    }

    Ok(ResolvedAuthTarget {
        auth_types: integration.auth_types.clone(),
        credential_fields: integration.credential_fields.clone(),
    })
}

fn prompt_connection_params<P>(
    connection_params: &BTreeMap<String, ConnectionParamDef>,
    prompts: &mut P,
) -> Result<Option<BTreeMap<String, String>>>
where
    P: ConnectPrompter,
{
    if connection_params.is_empty() {
        return Ok(None);
    }

    let mut values = BTreeMap::new();
    for (name, def) in connection_params {
        let value = prompts.input(&PromptInput {
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

fn prompt_manual_credentials<P>(
    fields: &[CredentialFieldInfo],
    prompts: &mut P,
) -> Result<ManualCredentialPayload>
where
    P: ConnectPrompter,
{
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
        let field = &fields[0];
        let value = prompts.input(&PromptInput {
            label: field.display_label(),
            description: non_empty(field.description.as_deref()).map(str::to_string),
            help_url: non_empty(field.help_url.as_deref()).map(str::to_string),
            default: None,
            required: true,
            secret: true,
        })?;
        return Ok(ManualCredentialPayload::Single(value));
    }

    let mut values = BTreeMap::new();
    for field in &fields {
        let value = prompts.input(&PromptInput {
            label: field.display_label(),
            description: non_empty(field.description.as_deref()).map(str::to_string),
            help_url: non_empty(field.help_url.as_deref()).map(str::to_string),
            default: None,
            required: true,
            secret: true,
        })?;
        values.insert(field.name.clone(), value);
    }

    Ok(ManualCredentialPayload::Multiple(values))
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
    let labels: Vec<&str> = auth_types
        .iter()
        .map(|auth_type| match auth_type.as_str() {
            "oauth" => "OAuth",
            "manual" => "manual",
            _ => auth_type.as_str(),
        })
        .collect();
    labels.join(" / ")
}

impl IntegrationInfo {
    fn display_name(&self) -> &str {
        non_empty(self.display_name.as_deref()).unwrap_or(&self.name)
    }

    fn connection_by_name(&self, name: &str) -> Option<&ConnectionDefInfo> {
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

#[cfg(test)]
mod tests {
    use anyhow::{Result, bail};
    use mockito::{Matcher, Server};

    use super::{
        ApiClient, ConnectPrompter, CredentialFieldInfo, PromptInput, PromptOption,
        connect_with_browser_opener_and_prompter,
    };

    struct MockPrompter {
        selections: Vec<usize>,
        inputs: Vec<String>,
        seen_selects: Vec<(String, Vec<PromptOption>)>,
        seen_inputs: Vec<PromptInput>,
    }

    impl MockPrompter {
        fn new(selections: Vec<usize>, inputs: Vec<&str>) -> Self {
            Self {
                selections,
                inputs: inputs.into_iter().map(str::to_string).collect(),
                seen_selects: Vec::new(),
                seen_inputs: Vec::new(),
            }
        }
    }

    impl ConnectPrompter for MockPrompter {
        fn select(&mut self, prompt: &str, options: &[PromptOption]) -> Result<usize> {
            self.seen_selects
                .push((prompt.to_string(), options.to_vec()));
            Ok(self.selections.remove(0))
        }

        fn input(&mut self, prompt: &PromptInput) -> Result<String> {
            self.seen_inputs.push(prompt.clone());
            Ok(self.inputs.remove(0))
        }
    }

    fn create_client(server: &Server) -> ApiClient {
        ApiClient::new(&server.url(), "test-token").unwrap()
    }

    #[test]
    fn manual_connect_uses_prompted_credentials_and_connection_params() {
        let mut server = Server::new();
        let _integrations = server
            .mock("GET", "/api/v1/integrations")
            .match_header("Authorization", "Bearer test-token")
            .with_status(200)
            .with_header("Content-Type", "application/json")
            .with_body(
                r#"[{
                    "name":"datadog",
                    "display_name":"Datadog",
                    "description":"Metrics and logs",
                    "connected":false,
                    "auth_types":["manual"],
                    "connection_params":{"site":{"description":"Datadog site","default":"datadoghq.com","required":true}},
                    "credential_fields":[{"name":"api_key","label":"API key","description":"Use a personal API key","help_url":"https://docs.example.com/datadog"}]
                }]"#,
            )
            .create();
        let _connect = server
            .mock("POST", "/api/v1/auth/connect-manual")
            .match_header("Authorization", "Bearer test-token")
            .match_header("Content-Type", "application/json")
            .match_body(Matcher::JsonString(
                r#"{"connection_params":{"site":"datadoghq.eu"},"credential":"dd-key","integration":"datadog"}"#.to_string(),
            ))
            .with_status(200)
            .with_header("Content-Type", "application/json")
            .with_body(r#"{"status":"connected","integration":"datadog"}"#)
            .create();

        let client = create_client(&server);
        let mut prompts = MockPrompter::new(vec![], vec!["datadoghq.eu", "dd-key"]);
        let browser_used = std::cell::Cell::new(false);

        let result = connect_with_browser_opener_and_prompter(
            &client,
            "datadog",
            None,
            None,
            &mut prompts,
            |_| {
                browser_used.set(true);
                Ok(())
            },
        );

        assert!(result.is_ok());
        assert!(!browser_used.get());
        assert_eq!(
            prompts.seen_inputs,
            vec![
                PromptInput {
                    label: "Datadog site".to_string(),
                    description: None,
                    help_url: None,
                    default: Some("datadoghq.com".to_string()),
                    required: true,
                    secret: false,
                },
                PromptInput {
                    label: "API key".to_string(),
                    description: Some("Use a personal API key".to_string()),
                    help_url: Some("https://docs.example.com/datadog".to_string()),
                    default: None,
                    required: true,
                    secret: true,
                },
            ]
        );
    }

    #[test]
    fn manual_connect_prompts_for_connection_and_finishes_candidate_selection() {
        let mut server = Server::new();
        let _integrations = server
            .mock("GET", "/api/v1/integrations")
            .match_header("Authorization", "Bearer test-token")
            .with_status(200)
            .with_header("Content-Type", "application/json")
            .with_body(
                r#"[{
                    "name":"manual-svc",
                    "display_name":"Manual Service",
                    "connected":false,
                    "connections":[
                        {"name":"workspace","auth_types":["manual"],"credential_fields":[{"name":"token","label":"Workspace token"}]},
                        {"name":"plugin","auth_types":["oauth"]}
                    ]
                }]"#,
            )
            .create();
        let _connect = server
            .mock("POST", "/api/v1/auth/connect-manual")
            .match_header("Authorization", "Bearer test-token")
            .match_header("Content-Type", "application/json")
            .match_body(Matcher::JsonString(
                r#"{"connection":"workspace","credential":"abc123","integration":"manual-svc"}"#
                    .to_string(),
            ))
            .with_status(200)
            .with_header("Content-Type", "application/json")
            .with_body(
                r#"{
                    "status":"selection_required",
                    "integration":"manual-svc",
                    "selection_url":"/api/v1/auth/pending-connection",
                    "pending_token":"pending-123",
                    "candidates":[
                        {"id":"site-a","name":"Site A"},
                        {"id":"site-b","name":"Site B"}
                    ]
                }"#,
            )
            .create();
        let _select = server
            .mock("POST", "/api/v1/auth/pending-connection")
            .match_header("Authorization", "Bearer test-token")
            .match_header(
                "content-type",
                Matcher::Regex("application/x-www-form-urlencoded.*".to_string()),
            )
            .match_body(Matcher::Exact(
                "pending_token=pending-123&candidate_index=1".to_string(),
            ))
            .with_status(200)
            .with_header("Content-Type", "text/html")
            .with_body("<html>ok</html>")
            .create();

        let client = create_client(&server);
        let mut prompts = MockPrompter::new(vec![0, 1], vec!["abc123"]);

        let result = connect_with_browser_opener_and_prompter(
            &client,
            "manual-svc",
            None,
            None,
            &mut prompts,
            |_| bail!("browser should not be used"),
        );

        assert!(result.is_ok());
        assert_eq!(prompts.seen_selects.len(), 2);
        assert_eq!(
            prompts.seen_selects[0].0,
            "Select a Manual Service connection:"
        );
        assert_eq!(
            prompts.seen_selects[0].1,
            vec![
                PromptOption {
                    label: "workspace".to_string(),
                    detail: Some("Auth: manual".to_string()),
                },
                PromptOption {
                    label: "plugin".to_string(),
                    detail: Some("Auth: OAuth".to_string()),
                },
            ]
        );
        assert_eq!(
            prompts.seen_inputs,
            vec![PromptInput {
                label: "Workspace token".to_string(),
                description: None,
                help_url: None,
                default: None,
                required: true,
                secret: true,
            }]
        );
    }

    #[test]
    fn oauth_connect_still_prefers_browser_flow() {
        let mut server = Server::new();
        let _integrations = server
            .mock("GET", "/api/v1/integrations")
            .match_header("Authorization", "Bearer test-token")
            .with_status(200)
            .with_header("Content-Type", "application/json")
            .with_body(
                r#"[{
                    "name":"github",
                    "display_name":"GitHub",
                    "connected":false,
                    "auth_types":["oauth","manual"],
                    "credential_fields":[{"name":"token","label":"Token"}]
                }]"#,
            )
            .create();
        let _oauth = server
            .mock("POST", "/api/v1/auth/start-oauth")
            .match_header("Authorization", "Bearer test-token")
            .match_header("Content-Type", "application/json")
            .match_body(Matcher::JsonString(
                r#"{"integration":"github"}"#.to_string(),
            ))
            .with_status(200)
            .with_header("Content-Type", "application/json")
            .with_body(r#"{"url":"https://example.com/oauth","state":"abc123"}"#)
            .create();

        let client = create_client(&server);
        let mut prompts = MockPrompter::new(vec![], vec![]);
        let opened = std::cell::Cell::new(String::new());

        let result = connect_with_browser_opener_and_prompter(
            &client,
            "github",
            None,
            None,
            &mut prompts,
            |url| {
                opened.set(url.to_string());
                Ok(())
            },
        );

        assert!(result.is_ok());
        assert_eq!(opened.take(), "https://example.com/oauth");
        assert!(prompts.seen_inputs.is_empty());
        assert!(prompts.seen_selects.is_empty());
    }

    #[test]
    fn prompt_manual_credentials_uses_generic_field_when_metadata_missing() {
        let mut prompts = MockPrompter::new(vec![], vec!["secret"]);
        let result = super::prompt_manual_credentials(&[], &mut prompts).unwrap();

        assert_eq!(
            prompts.seen_inputs,
            vec![PromptInput {
                label: "Credential".to_string(),
                description: None,
                help_url: None,
                default: None,
                required: true,
                secret: true,
            }]
        );
        match result {
            super::ManualCredentialPayload::Single(value) => assert_eq!(value, "secret"),
            super::ManualCredentialPayload::Multiple(_) => panic!("expected single credential"),
        }
        assert_eq!(
            CredentialFieldInfo {
                name: "token".to_string(),
                label: Some("API token".to_string()),
                description: None,
                help_url: None,
            }
            .display_label(),
            "API token"
        );
    }
}
