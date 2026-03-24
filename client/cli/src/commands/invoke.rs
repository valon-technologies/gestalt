use anyhow::{Context, Result};
use serde::Deserialize;

use crate::api::ApiClient;
use crate::output::{self, Format};

#[derive(Deserialize)]
#[serde(rename_all = "PascalCase")]
struct Operation {
    name: String,
    description: String,
    method: String,
    parameters: Vec<Parameter>,
}

#[derive(Deserialize)]
#[serde(rename_all = "PascalCase")]
struct Parameter {
    name: String,
    #[serde(default = "default_type")]
    r#type: String,
    #[serde(default)]
    required: bool,
}

fn default_type() -> String {
    "string".to_string()
}

pub fn invoke(
    url_override: Option<&str>,
    integration: &str,
    operation: &str,
    params: &[(String, String)],
    format: Format,
) -> Result<()> {
    let client = ApiClient::from_env(url_override)?;
    let path = format!("/api/v1/{}/{}", integration, operation);

    let resp = if params.is_empty() {
        client
            .get(&path)
            .with_context(|| format!("failed to invoke {}.{}", integration, operation))?
    } else {
        client
            .post_params(&path, params)
            .with_context(|| format!("failed to invoke {}.{}", integration, operation))?
    };

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => output::print_json_table(&resp),
    }

    Ok(())
}

pub fn list_operations(
    url_override: Option<&str>,
    integration: &str,
    format: Format,
) -> Result<()> {
    let client = ApiClient::from_env(url_override)?;
    let path = format!("/api/v1/integrations/{}/operations", integration);
    let resp = client
        .get(&path)
        .with_context(|| format!("failed to list operations for {}", integration))?;

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            let operations: Vec<Operation> =
                serde_json::from_value(resp).context("failed to parse operations response")?;
            let rows: Vec<Vec<String>> = operations
                .into_iter()
                .map(|op| {
                    let params = format_parameters(&op.parameters);
                    vec![op.name, op.description, op.method, params]
                })
                .collect();
            output::print_table(&["Name", "Description", "Method", "Parameters"], &rows);
        }
    }

    Ok(())
}

fn format_parameters(params: &[Parameter]) -> String {
    params
        .iter()
        .map(|p| {
            let mut s = format!("-p {}=<{}>", p.name, p.r#type);
            if p.required {
                s.push_str(" (required)");
            }
            s
        })
        .collect::<Vec<_>>()
        .join(", ")
}
