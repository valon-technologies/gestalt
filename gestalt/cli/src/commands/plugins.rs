mod connect;

use anyhow::{Context, Result};

use crate::api::ApiClient;
use crate::output::{self, Format};

pub use connect::{
    connect, connect_managed_subject, connect_managed_subject_with_browser_opener,
    connect_with_browser_opener,
};

const PLUGIN_CONNECTION_NAME: &str = "_plugin";
const PLUGIN_CONNECTION_ALIAS: &str = "plugin";
const PLUGIN_GROUP_DIVIDER: char = '┄';
const PLUGIN_GROUP_DIVIDER_SENTINEL: &str = "\u{E000}";
const PLUGIN_LIST_HEADERS: [&str; 5] = ["Name", "Description", "Connection", "Instance", "Status"];

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

pub fn canonical_connection_name(name: &str) -> &str {
    ConnectionName::canonical_of(name)
}

pub fn list(client: &ApiClient, format: Format) -> Result<()> {
    let resp = client
        .get("/api/v1/integrations")
        .context("failed to list plugins")?;

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            let mut rows = Vec::new();
            for (index, item) in resp.as_array().unwrap_or(&Vec::new()).iter().enumerate() {
                if index > 0 {
                    rows.push(plugin_group_divider_row());
                }
                rows.extend(plugin_rows(item));
            }
            print_plugin_list_table(&rows);
        }
    }
    Ok(())
}

fn print_plugin_list_table(rows: &[Vec<String>]) {
    println!("{}", render_plugin_list_table(rows));
}

fn render_plugin_list_table(rows: &[Vec<String>]) -> String {
    let rendered = output::render_table(&PLUGIN_LIST_HEADERS, rows);
    rendered
        .lines()
        .map(expand_plugin_group_divider_line)
        .collect::<Vec<_>>()
        .join("\n")
}

fn expand_plugin_group_divider_line(line: &str) -> String {
    if !line.contains(PLUGIN_GROUP_DIVIDER_SENTINEL) {
        return line.to_string();
    }

    let Some(left_border) = line.find('│') else {
        return line.to_string();
    };
    let Some(right_border) = line.rfind('│').filter(|index| *index > left_border) else {
        return line.to_string();
    };

    let interior_width = line[left_border + '│'.len_utf8()..right_border]
        .chars()
        .count();
    format!(
        "{}{}{}",
        &line[..left_border + '│'.len_utf8()],
        PLUGIN_GROUP_DIVIDER.to_string().repeat(interior_width),
        &line[right_border..],
    )
}

fn plugin_status(item: &serde_json::Value) -> String {
    item["status"]
        .as_str()
        .map(str::to_string)
        .unwrap_or_else(|| "unknown".to_string())
}

fn plugin_rows(item: &serde_json::Value) -> Vec<Vec<String>> {
    let mut connections = plugin_connection_rows(item);
    if connections.is_empty() {
        connections.push((plugin_status(item), "-".to_string(), "-".to_string()));
    }

    connections
        .into_iter()
        .enumerate()
        .map(|(index, (status, connection, instance))| {
            vec![
                if index == 0 {
                    item["name"].as_str().unwrap_or("-").to_string()
                } else {
                    String::new()
                },
                if index == 0 {
                    item["description"].as_str().unwrap_or("-").to_string()
                } else {
                    String::new()
                },
                connection,
                instance,
                status,
            ]
        })
        .collect()
}

fn plugin_group_divider_row() -> Vec<String> {
    vec![
        PLUGIN_GROUP_DIVIDER_SENTINEL.to_string(),
        String::new(),
        String::new(),
        String::new(),
        String::new(),
    ]
}

fn plugin_connection_rows(item: &serde_json::Value) -> Vec<(String, String, String)> {
    item["connections"]
        .as_array()
        .map(|connections| connections.iter().flat_map(connection_rows).collect())
        .unwrap_or_default()
}

fn connection_rows(connection: &serde_json::Value) -> Vec<(String, String, String)> {
    let name = connection["name"].as_str().unwrap_or("-");
    if let Some(instances) = connection["instances"]
        .as_array()
        .filter(|instances| !instances.is_empty())
    {
        let rows: Vec<_> = instances
            .iter()
            .filter_map(|instance| {
                let instance_name = instance["name"].as_str()?;
                let connection_name = instance["connection"].as_str().unwrap_or(name);
                Some((
                    connection_status(connection, Some(instance)),
                    connection_name.to_string(),
                    instance_name.to_string(),
                ))
            })
            .collect();
        if !rows.is_empty() {
            return rows;
        }
    }

    vec![(
        connection_status(connection, None),
        name.to_string(),
        "-".to_string(),
    )]
}

fn connection_status(
    connection: &serde_json::Value,
    instance: Option<&serde_json::Value>,
) -> String {
    instance
        .and_then(|instance| {
            instance["credentialState"]
                .as_str()
                .or_else(|| instance["status"].as_str())
        })
        .or_else(|| connection["credentialState"].as_str())
        .or_else(|| connection["status"].as_str())
        .unwrap_or("unknown")
        .to_string()
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
