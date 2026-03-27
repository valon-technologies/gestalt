use anyhow::{Context, Result};

use crate::api::ApiClient;
use crate::catalog;
use crate::output::{self, Format};
use crate::params::{self, ParamEntry};

pub fn invoke(
    url_override: Option<&str>,
    integration: &str,
    operation: &str,
    params: &[ParamEntry],
    select: Option<&str>,
    input_file: Option<&str>,
    format: Format,
) -> Result<()> {
    let client = ApiClient::from_env(url_override)?;
    let cat = catalog::fetch_catalog(&client, integration)?;
    let mut param_map = params::assemble_params(params, Some(&cat), operation)?;

    if let Some(file_path) = input_file {
        let file_map = params::load_input_file(file_path)?;
        param_map = params::merge_params(file_map, param_map);
    }

    let path = format!("/api/v1/{}/{}", integration, operation);
    let resp = if param_map.is_empty() {
        client
            .get(&path)
            .with_context(|| format!("failed to invoke {}.{}", integration, operation))?
    } else {
        client
            .post(&path, &serde_json::Value::Object(param_map))
            .with_context(|| format!("failed to invoke {}.{}", integration, operation))?
    };

    let output_value = match select {
        Some(sel_path) => output::select_path(&resp, sel_path)?,
        None => resp,
    };

    match format {
        Format::Json => output::print_json(&output_value),
        Format::Table => output::print_json_table(&output_value),
    }

    Ok(())
}

pub fn list_operations(
    url_override: Option<&str>,
    integration: &str,
    format: Format,
) -> Result<()> {
    let client = ApiClient::from_env(url_override)?;
    let cat = catalog::fetch_catalog(&client, integration)?;

    match format {
        Format::Json => {
            output::print_json(&serde_json::to_value(cat.operations()).unwrap());
        }
        Format::Table => {
            let rows: Vec<Vec<String>> = cat
                .operations()
                .iter()
                .map(|op| {
                    let params = format_parameters(&op.parameters);
                    vec![
                        op.id.clone(),
                        op.description.clone(),
                        op.method.clone(),
                        params,
                    ]
                })
                .collect();
            output::print_table(&["Name", "Description", "Method", "Parameters"], &rows);
        }
    }

    Ok(())
}

fn format_parameters(params: &[catalog::CatalogParameter]) -> String {
    params
        .iter()
        .map(|p| {
            let location_hint = if p.location.is_empty() {
                String::new()
            } else {
                format!(" [{}]", p.location)
            };
            let mut s = format!("-p {}=<{}>{}", p.name, p.r#type, location_hint);
            if p.required {
                s.push_str(" (required)");
            }
            s
        })
        .collect::<Vec<_>>()
        .join(", ")
}
