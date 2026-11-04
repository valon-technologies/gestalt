use anyhow::{Context, Result, bail};
use serde::Deserialize;

use crate::api::{ApiClient, encode_path_segment};
use crate::output::{self, Format};

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct CheckResponse {
    app: String,
    ok: bool,
    #[serde(default)]
    surfaces: Vec<SurfaceCheck>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct SurfaceCheck {
    upstream: String,
    #[serde(default)]
    tools: Vec<String>,
    #[serde(default)]
    allowed_operations_configured: bool,
    #[serde(default)]
    exposed: Vec<ExposedTool>,
    #[serde(default)]
    unknown_allowed_operations: Vec<String>,
    #[serde(default)]
    unexposed_tools: Vec<String>,
    #[serde(default)]
    error: String,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct ExposedTool {
    tool: String,
    exposed_as: String,
}

/// Lists the app's MCP tools live and checks its allowedOperations against
/// them. Fails when any allowedOperations key names no advertised tool or an
/// MCP upstream could not be listed.
pub fn check(
    client: &ApiClient,
    app: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    format: Format,
) -> Result<()> {
    let value = client.get(&check_path(app, connection, instance)?)?;
    let response: CheckResponse =
        serde_json::from_value(value.clone()).context("failed to parse check response")?;

    match format {
        Format::Json => output::print_json(&value),
        Format::Table => print_report(&response),
    }

    if !response.ok {
        bail!("{}", failure_summary(&response));
    }
    if matches!(format, Format::Table) {
        output::print_success(&format!(
            "allowedOperations for '{}' match the live MCP tools.",
            response.app
        ));
    }
    Ok(())
}

fn check_path(app: &str, connection: Option<&str>, instance: Option<&str>) -> Result<String> {
    let mut path = format!("/api/v1/apps/{}/admin/check", encode_path_segment(app));
    let mut params = Vec::new();
    if let Some(connection) = connection {
        params.push(("_connection", connection));
    }
    if let Some(instance) = instance {
        params.push(("_instance", instance));
    }
    if !params.is_empty() {
        let query = serde_urlencoded::to_string(params).context("failed to encode selectors")?;
        path.push('?');
        path.push_str(&query);
    }
    Ok(path)
}

fn print_report(response: &CheckResponse) {
    println!("App: {}", response.app);
    for surface in &response.surfaces {
        println!();
        if !surface.error.is_empty() {
            println!("MCP upstream {}: could not list tools", surface.upstream);
            println!("  {}", surface.error);
            continue;
        }
        println!(
            "MCP upstream {}: {} tool{} advertised",
            surface.upstream,
            surface.tools.len(),
            plural(surface.tools.len())
        );
        if !surface.allowed_operations_configured {
            println!("  No allowedOperations configured; every tool is exposed as-is.");
            continue;
        }

        if !surface.unknown_allowed_operations.is_empty() {
            println!();
            println!("  allowedOperations keys that match no tool:");
            for key in &surface.unknown_allowed_operations {
                match closest_tool(key, &surface.tools) {
                    Some(tool) => println!("    {key}  (did you mean {tool}?)"),
                    None => println!("    {key}"),
                }
            }
        }

        if !surface.exposed.is_empty() {
            println!();
            println!("  Exposed ({}):", surface.exposed.len());
            let rows: Vec<Vec<String>> = surface
                .exposed
                .iter()
                .map(|tool| vec![tool.tool.clone(), tool.exposed_as.clone()])
                .collect();
            output::print_table(&["Tool", "Exposed As"], &rows);
        }

        if !surface.unexposed_tools.is_empty() {
            println!();
            println!(
                "  Hidden by allowedOperations ({}): {}",
                surface.unexposed_tools.len(),
                surface.unexposed_tools.join(", ")
            );
        }
    }
    println!();
}

fn failure_summary(response: &CheckResponse) -> String {
    let unknown: usize = response
        .surfaces
        .iter()
        .map(|surface| surface.unknown_allowed_operations.len())
        .sum();
    let failed: Vec<&str> = response
        .surfaces
        .iter()
        .filter(|surface| !surface.error.is_empty())
        .map(|surface| surface.upstream.as_str())
        .collect();

    let mut parts = Vec::new();
    if unknown > 0 {
        parts.push(format!(
            "{unknown} allowedOperations key{} match{} no MCP tool; until fixed, listing '{}' operations fails for every user",
            plural(unknown),
            if unknown == 1 { "es" } else { "" },
            response.app
        ));
    }
    if !failed.is_empty() {
        parts.push(format!("could not list tools from {}", failed.join(", ")));
    }
    if parts.is_empty() {
        return format!("check failed for '{}'", response.app);
    }
    parts.join("; ")
}

fn plural(n: usize) -> &'static str {
    if n == 1 { "" } else { "s" }
}

/// Returns the advertised tool closest to `key` when it is plausibly a typo.
fn closest_tool<'a>(key: &str, tools: &'a [String]) -> Option<&'a str> {
    let threshold = (key.chars().count() / 4).max(2);
    tools
        .iter()
        .map(|tool| (edit_distance(key, tool), tool))
        .filter(|(distance, _)| *distance <= threshold)
        .min_by_key(|(distance, _)| *distance)
        .map(|(_, tool)| tool.as_str())
}

fn edit_distance(a: &str, b: &str) -> usize {
    let b: Vec<char> = b.chars().collect();
    let mut prev: Vec<usize> = (0..=b.len()).collect();
    for (i, ca) in a.chars().enumerate() {
        let mut curr = vec![i + 1; b.len() + 1];
        for (j, cb) in b.iter().enumerate() {
            let cost = usize::from(ca != *cb);
            curr[j + 1] = (prev[j] + cost).min(prev[j + 1] + 1).min(curr[j] + 1);
        }
        prev = curr;
    }
    prev[b.len()]
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn suggests_close_tool_names() {
        let tools = vec!["list_issues".to_string(), "create_issue".to_string()];
        assert_eq!(closest_tool("list_isssues", &tools), Some("list_issues"));
        assert_eq!(closest_tool("delete_project", &tools), None);
    }

    #[test]
    fn encodes_selectors_in_path() {
        assert_eq!(
            check_path("linear", Some("work space"), None).unwrap(),
            "/api/v1/apps/linear/admin/check?_connection=work+space"
        );
        assert_eq!(
            check_path("linear", None, None).unwrap(),
            "/api/v1/apps/linear/admin/check"
        );
    }

    #[test]
    fn summarizes_unknown_keys_and_unreachable_upstreams() {
        let response: CheckResponse = serde_json::from_value(serde_json::json!({
            "app": "linear",
            "ok": false,
            "surfaces": [
                {"upstream": "linear", "unknownAllowedOperations": ["a", "b"]},
                {"upstream": "linear-mcp", "error": "401"}
            ]
        }))
        .unwrap();
        let summary = failure_summary(&response);
        assert!(
            summary.starts_with("2 allowedOperations keys match no MCP tool"),
            "{summary}"
        );
        assert!(
            summary.ends_with("could not list tools from linear-mcp"),
            "{summary}"
        );
    }
}
