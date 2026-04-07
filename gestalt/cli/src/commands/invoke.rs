use anyhow::{Context, Result};

use crate::api::ApiClient;
use crate::catalog;
use crate::output::{self, Format};
use crate::params::{self, ParamEntry};

#[derive(Default)]
pub struct InvokeOptions<'a> {
    pub connection: Option<&'a str>,
    pub instance: Option<&'a str>,
    pub select: Option<&'a str>,
    pub input_file: Option<&'a str>,
}

pub fn invoke(
    client: &ApiClient,
    integration: &str,
    operation: &str,
    params: &[ParamEntry],
    options: InvokeOptions<'_>,
    format: Format,
) -> Result<()> {
    let cat = catalog::fetch_catalog(client, integration)?;
    let mut param_map = params::assemble_params(params, Some(&cat), operation)?;

    if let Some(file_path) = options.input_file {
        let file_map = params::load_input_file(file_path)?;
        param_map = params::merge_params(file_map, param_map);
    }

    if let Some(connection) = options.connection {
        param_map.insert(
            "_connection".to_string(),
            serde_json::Value::String(connection.to_string()),
        );
    }
    if let Some(instance) = options.instance {
        param_map.insert(
            "_instance".to_string(),
            serde_json::Value::String(instance.to_string()),
        );
    }

    let path = format!("/api/v1/{}/{}", integration, operation);
    let resp = (if param_map.is_empty() {
        client.get(&path)
    } else {
        client.post(&path, &serde_json::Value::Object(param_map))
    })
    .map_err(|err| {
        let message = err.to_string();
        if !message.contains("no token stored for integration") {
            return err;
        }

        let mut connect_command = format!("gestalt integrations connect {}", integration);
        if let Some(connection) = options.connection {
            connect_command.push_str(" --connection ");
            connect_command.push_str(connection);
        }
        if let Some(instance) = options.instance {
            connect_command.push_str(" --instance ");
            connect_command.push_str(instance);
        }

        anyhow::anyhow!(
            "integration {:?} is not connected. Connect it first with `{}`",
            integration,
            connect_command,
        )
    })
    .with_context(|| format!("failed to invoke {}.{}", integration, operation))?;

    let output_value = match options.select {
        Some(sel_path) => output::select_path(&resp, sel_path)?,
        None => resp,
    };

    match format {
        Format::Json => output::print_json(&output_value),
        Format::Table => output::print_json_table(&output_value),
    }

    Ok(())
}

pub fn list_operations(client: &ApiClient, integration: &str, format: Format) -> Result<()> {
    let cat = catalog::fetch_catalog(client, integration)?;

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
