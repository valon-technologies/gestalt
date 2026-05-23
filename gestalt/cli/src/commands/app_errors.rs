use std::collections::BTreeSet;

use anyhow::Result;

use crate::{
    api::{ApiClient, ApiError},
    output,
};

#[derive(PartialEq, Eq)]
pub(crate) enum ConnectErrorKind {
    NotConnected,
    ReconnectRequired,
    InstanceSelectionRequired,
}

#[derive(Clone, Copy)]
pub(crate) enum SelectorCommand {
    Invoke,
    Describe,
}

pub(crate) fn should_retry_without_catalog(err: &anyhow::Error, operation: &str) -> bool {
    matches!(
        connect_error_kind(err),
        Some(ConnectErrorKind::NotConnected | ConnectErrorKind::ReconnectRequired)
    ) && !operation.is_empty()
}

pub(crate) fn map_catalog_error(
    client: &ApiClient,
    plugin: &str,
    operation: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    selector_command: SelectorCommand,
    result: Result<crate::catalog::OperationsCatalog>,
) -> Result<crate::catalog::OperationsCatalog> {
    result.map_err(|err| {
        rewrite_connect_error(
            client,
            err,
            plugin,
            operation,
            connection,
            instance,
            selector_command,
        )
    })
}

pub(crate) fn rewrite_connect_error(
    client: &ApiClient,
    err: anyhow::Error,
    plugin: &str,
    operation: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    selector_command: SelectorCommand,
) -> anyhow::Error {
    let connect_command = connect_command(plugin, connection, instance);

    match connect_error_kind(&err) {
        Some(ConnectErrorKind::NotConnected) => anyhow::anyhow!(
            "app {:?} is not connected. Connect it first with `{}`",
            plugin,
            connect_command,
        ),
        Some(ConnectErrorKind::ReconnectRequired) => anyhow::anyhow!(
            "token for app {:?} expired or was revoked. Reconnect it with `{}`",
            plugin,
            connect_command,
        ),
        Some(ConnectErrorKind::InstanceSelectionRequired) => {
            instance_selection_message(client, plugin, operation, connection, selector_command)
                .map(anyhow::Error::msg)
                .unwrap_or_else(|| {
                    anyhow::anyhow!(
                        "app {:?} has multiple connected instances. Pass --connection and --instance to choose one",
                        plugin,
                    )
                })
        }
        None => err,
    }
}

fn connect_command(plugin: &str, connection: Option<&str>, instance: Option<&str>) -> String {
    let mut connect_command = format!("gestalt app connect {}", shell_word(plugin));
    if let Some(connection) = connection {
        connect_command.push_str(" --connection ");
        connect_command.push_str(&shell_word(connection));
    }
    if let Some(instance) = instance {
        connect_command.push_str(" --instance ");
        connect_command.push_str(&shell_word(instance));
    }
    connect_command
}

fn connect_error_kind(err: &anyhow::Error) -> Option<ConnectErrorKind> {
    for cause in err.chain() {
        if let Some(api_error) = cause.downcast_ref::<ApiError>() {
            match api_error.code() {
                Some("not_connected") => return Some(ConnectErrorKind::NotConnected),
                Some("reconnect_required") => return Some(ConnectErrorKind::ReconnectRequired),
                Some("instance_selection_required") => {
                    return Some(ConnectErrorKind::InstanceSelectionRequired);
                }
                _ => {}
            }
        }

        let message = cause.to_string();
        if message.contains("no token stored for integration") {
            return Some(ConnectErrorKind::NotConnected);
        }
        if message.contains("is not connected. Connect it first with `") {
            return Some(ConnectErrorKind::NotConnected);
        }
        if message.contains("expired or was revoked") {
            return Some(ConnectErrorKind::ReconnectRequired);
        }
        let lower_message = message.to_ascii_lowercase();
        if lower_message.contains("specify which instance")
            || lower_message.contains("pass --instance")
        {
            return Some(ConnectErrorKind::InstanceSelectionRequired);
        }
    }
    None
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
pub(crate) struct ResolvedSelector {
    pub(crate) connection: String,
    pub(crate) instance: String,
}

pub(crate) enum SelectorResolution {
    Selected(ResolvedSelector),
    Message(String),
    Unchanged,
}

fn instance_selection_message(
    client: &ApiClient,
    plugin: &str,
    operation: &str,
    connection: Option<&str>,
    selector_command: SelectorCommand,
) -> Option<String> {
    let integrations = client.get("/api/v1/apps").ok()?;
    let pairs =
        selector_pairs_for_plugin_matching_selector(&integrations, plugin, connection, None);
    match selector_resolution_from_pairs(plugin, operation, selector_command, pairs) {
        SelectorResolution::Message(message) => Some(message),
        SelectorResolution::Selected(_) | SelectorResolution::Unchanged => None,
    }
}

pub(crate) fn resolve_selector(
    client: &ApiClient,
    plugin: &str,
    operation: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    selector_command: SelectorCommand,
) -> SelectorResolution {
    if connection.is_some() && instance.is_some() {
        return SelectorResolution::Unchanged;
    }
    let Ok(integrations) = client.get("/api/v1/apps") else {
        return SelectorResolution::Unchanged;
    };
    let pairs =
        selector_pairs_for_plugin_matching_selector(&integrations, plugin, connection, instance);
    selector_resolution_from_pairs(plugin, operation, selector_command, pairs)
}

fn selector_resolution_from_pairs(
    plugin: &str,
    operation: &str,
    selector_command: SelectorCommand,
    pairs: Vec<ResolvedSelector>,
) -> SelectorResolution {
    if pairs.len() >= 2 {
        let rows: Vec<Vec<String>> = pairs
            .iter()
            .map(|pair| {
                vec![
                    pair.connection.clone(),
                    pair.instance.clone(),
                    selector_command.render(plugin, operation, pair),
                ]
            })
            .collect();
        let selectors = output::render_table(&["Connection", "Instance", "Example"], &rows);
        return SelectorResolution::Message(format!(
            "app {:?} has multiple connected instances. Choose a connection and instance.\n\n{}",
            plugin, selectors
        ));
    }

    if pairs.len() == 1 {
        return SelectorResolution::Selected(pairs.into_iter().next().unwrap());
    }

    SelectorResolution::Unchanged
}

fn selector_pairs_for_plugin_matching_selector(
    integrations: &serde_json::Value,
    plugin: &str,
    connection_filter: Option<&str>,
    instance_filter: Option<&str>,
) -> Vec<ResolvedSelector> {
    let mut unique = BTreeSet::new();

    if let Some(item) = integrations.as_array().and_then(|items| {
        items
            .iter()
            .find(|item| item["name"].as_str() == Some(plugin))
    }) && let Some(connections) = item["connections"].as_array()
    {
        for connection in connections {
            let connection_name = connection["name"].as_str().unwrap_or("");
            if let Some(instances) = connection["instances"].as_array() {
                for instance in instances {
                    let Some(instance_name) = instance["name"].as_str() else {
                        continue;
                    };
                    if !is_connected_selector(connection, instance) {
                        continue;
                    }
                    let connection_name =
                        instance["connection"].as_str().unwrap_or(connection_name);
                    if connection_filter.is_some_and(|filter| filter != connection_name) {
                        continue;
                    }
                    if instance_filter.is_some_and(|filter| filter != instance_name) {
                        continue;
                    }
                    if connection_name.is_empty() || instance_name.is_empty() {
                        continue;
                    }
                    unique.insert(ResolvedSelector {
                        connection: connection_name.to_string(),
                        instance: instance_name.to_string(),
                    });
                }
            }
        }
    }

    unique.into_iter().collect()
}

fn is_connected_selector(connection: &serde_json::Value, instance: &serde_json::Value) -> bool {
    let status = instance["credentialState"]
        .as_str()
        .or_else(|| instance["status"].as_str())
        .or_else(|| connection["credentialState"].as_str())
        .or_else(|| connection["status"].as_str());
    matches!(status, Some("connected" | "ready"))
}

impl SelectorCommand {
    fn render(self, plugin: &str, operation: &str, pair: &ResolvedSelector) -> String {
        let mut command = match self {
            SelectorCommand::Invoke => format!("gestalt app invoke {}", shell_word(plugin)),
            SelectorCommand::Describe => format!(
                "gestalt app describe {} {}",
                shell_word(plugin),
                shell_word(operation)
            ),
        };
        if matches!(self, SelectorCommand::Invoke) && !operation.is_empty() {
            command.push(' ');
            command.push_str(&shell_word(operation));
        }
        command.push_str(" --connection ");
        command.push_str(&shell_word(&pair.connection));
        command.push_str(" --instance ");
        command.push_str(&shell_word(&pair.instance));
        command
    }
}

fn shell_word(value: &str) -> String {
    if !value.is_empty()
        && !value.starts_with('-')
        && value
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b"@%_+=:,./-".contains(&b))
    {
        return value.to_string();
    }
    format!("'{}'", value.replace('\'', "'\\''"))
}
