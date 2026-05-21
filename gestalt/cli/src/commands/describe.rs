use anyhow::{Result, bail};

use crate::api::ApiClient;
use crate::catalog;
use crate::output::{self, Format};

use super::plugin_errors;

pub fn describe(
    client: &ApiClient,
    plugin: &str,
    operation: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    format: Format,
) -> Result<()> {
    let selector_resolution = plugin_errors::resolve_selector(
        client,
        plugin,
        operation,
        connection,
        instance,
        plugin_errors::SelectorCommand::Describe,
    );
    let resolved_selector = match selector_resolution {
        plugin_errors::SelectorResolution::Selected(selector) => Some(selector),
        plugin_errors::SelectorResolution::Message(message) => bail!(message),
        plugin_errors::SelectorResolution::Unchanged => None,
    };
    let connection = resolved_selector
        .as_ref()
        .map(|selector| selector.connection.as_str())
        .or(connection);
    let instance = resolved_selector
        .as_ref()
        .map(|selector| selector.instance.as_str())
        .or(instance);

    let cat = plugin_errors::map_catalog_error(
        client,
        plugin,
        operation,
        connection,
        instance,
        plugin_errors::SelectorCommand::Describe,
        catalog::fetch_catalog(client, plugin, connection, instance),
    )?;

    let op = match cat.find_operation(operation) {
        Some(op) => op.clone(),
        None if connection.is_some() || instance.is_some() || cat.operations().is_empty() => {
            let fallback_cat = plugin_errors::map_catalog_error(
                client,
                plugin,
                operation,
                None,
                None,
                plugin_errors::SelectorCommand::Describe,
                catalog::fetch_catalog(client, plugin, None, None),
            )?;
            match fallback_cat.find_operation(operation) {
                Some(op) => op.clone(),
                None => {
                    let available: Vec<&str> = fallback_cat
                        .operations()
                        .iter()
                        .map(|o| o.id.as_str())
                        .collect();
                    bail!(
                        "operation '{}' not found; available operations: {}",
                        operation,
                        available.join(", ")
                    );
                }
            }
        }
        None => {
            let available: Vec<&str> = cat.operations().iter().map(|o| o.id.as_str()).collect();
            bail!(
                "operation '{}' not found; available operations: {}",
                operation,
                available.join(", ")
            );
        }
    };

    match format {
        Format::Json => {
            let val = serde_json::to_value(&op)?;
            output::print_json(&val);
        }
        Format::Table => {
            println!("Operation:   {}", op.id);
            if let Some(connection) = connection {
                println!("Connection:  {connection}");
            }
            if let Some(instance) = instance {
                println!("Instance:    {instance}");
            }
            if !op.transport.is_empty() {
                println!("Transport:   {}", op.transport);
            }
            if !op.method.is_empty() {
                println!("Method:      {}", op.method);
            }
            if !op.title.is_empty() {
                println!("Title:       {}", op.title);
            }
            if !op.description.is_empty() {
                println!("Description: {}", op.description);
            }
            println!();

            if op.parameters.is_empty() {
                println!("Parameters:  (none)");
            } else {
                println!("Parameters:");
                let headers = &["Name", "Type", "Location", "Required"];
                let rows: Vec<Vec<String>> = op
                    .parameters
                    .iter()
                    .map(|p| {
                        vec![
                            p.name.clone(),
                            p.r#type.clone(),
                            p.location.clone(),
                            if p.required { "yes" } else { "no" }.to_string(),
                        ]
                    })
                    .collect();
                output::print_table(headers, &rows);
            }
            println!();
            println!(
                "Has structured input schema: {}",
                if op.input_schema.is_some() {
                    "yes"
                } else {
                    "no"
                }
            );
        }
    }

    Ok(())
}
