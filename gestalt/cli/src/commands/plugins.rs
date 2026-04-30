mod connect;

use anyhow::{Context, Result};

use crate::api::ApiClient;
use crate::output::{self, Format};

pub use connect::{connect, connect_with_browser_opener};

const PLUGIN_CONNECTION_NAME: &str = "_plugin";
const PLUGIN_CONNECTION_ALIAS: &str = "plugin";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct ConnectionName<'a> {
    original: &'a str,
}

impl<'a> ConnectionName<'a> {
    fn new(name: &'a str) -> Self {
        Self { original: name }
    }

    fn canonical_of(name: &str) -> &str {
        if name == PLUGIN_CONNECTION_ALIAS {
            PLUGIN_CONNECTION_NAME
        } else {
            name
        }
    }

    fn canonical(self) -> &'a str {
        Self::canonical_of(self.original)
    }

    fn display(self) -> &'a str {
        if self.canonical() == PLUGIN_CONNECTION_NAME {
            PLUGIN_CONNECTION_ALIAS
        } else {
            self.canonical()
        }
    }

    fn matches(self, other: &str) -> bool {
        self.canonical() == Self::canonical_of(other)
    }
}

pub fn list(client: &ApiClient, format: Format) -> Result<()> {
    let resp = client
        .get("/api/v1/integrations")
        .context("failed to list plugins")?;

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            let rows: Vec<Vec<String>> = resp
                .as_array()
                .unwrap_or(&Vec::new())
                .iter()
                .map(|item| {
                    vec![
                        item["name"].as_str().unwrap_or("-").to_string(),
                        item["description"].as_str().unwrap_or("-").to_string(),
                        plugin_status(item),
                        plugin_connections(item),
                    ]
                })
                .collect();
            output::print_table(&["Name", "Description", "Status", "Connections"], &rows);
        }
    }
    Ok(())
}

fn plugin_status(item: &serde_json::Value) -> String {
    item["status"]
        .as_str()
        .map(str::to_string)
        .unwrap_or_else(|| match item["connected"].as_bool() {
            Some(true) => "ready".to_string(),
            _ => "needs_user_connection".to_string(),
        })
}

fn plugin_connections(item: &serde_json::Value) -> String {
    if let Some(connections) = item["connections"]
        .as_array()
        .filter(|connections| !connections.is_empty())
    {
        return connections
            .iter()
            .map(|connection| {
                let name = connection["name"].as_str().unwrap_or("-");
                let status = connection["status"]
                    .as_str()
                    .map(str::to_string)
                    .unwrap_or_else(|| match connection["connected"].as_bool() {
                        Some(true) => "ready".to_string(),
                        _ => "needs_user_connection".to_string(),
                    });
                format!("{name}: {status}")
            })
            .collect::<Vec<_>>()
            .join(", ");
    }

    let instance_count = item["instances"].as_array().map(Vec::len).unwrap_or(0);
    match instance_count {
        0 => "-".to_string(),
        1 => "1 instance".to_string(),
        n => format!("{n} instances"),
    }
}

pub fn disconnect(
    client: &ApiClient,
    name: &str,
    connection: Option<&str>,
    instance: Option<&str>,
) -> Result<()> {
    let normalized_connection = connection.map(|value| ConnectionName::new(value).canonical());
    let mut path = format!("/api/v1/integrations/{name}");
    let params: Vec<(&str, &str)> = [
        ("_connection", normalized_connection),
        ("_instance", instance),
    ]
    .into_iter()
    .filter_map(|(key, value)| value.map(|v| (key, v)))
    .collect();
    if !params.is_empty() {
        let query = serde_urlencoded::to_string(&params).context("failed to encode query")?;
        path = format!("{path}?{query}");
    }

    client
        .delete(&path)
        .with_context(|| format!("failed to disconnect plugin '{}'", name))?;

    output::print_success(&format!("Disconnected {}.", name));
    Ok(())
}
