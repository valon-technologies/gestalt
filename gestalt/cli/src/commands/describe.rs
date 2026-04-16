use anyhow::{Result, bail};

use crate::api::ApiClient;
use crate::catalog::{self, CatalogSelectors};
use crate::output::{self, Format};

#[derive(Default)]
pub struct DescribeOptions<'a> {
    pub connection: Option<&'a str>,
    pub instance: Option<&'a str>,
}

pub fn describe(
    client: &ApiClient,
    plugin: &str,
    operation: &str,
    options: DescribeOptions<'_>,
    format: Format,
) -> Result<()> {
    let cat = catalog::fetch_catalog(
        client,
        plugin,
        CatalogSelectors {
            connection: options.connection,
            instance: options.instance,
        },
    )?;

    let op = match cat.find_operation(operation) {
        Some(op) => op,
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
            let val = serde_json::to_value(op)?;
            output::print_json(&val);
        }
        Format::Table => {
            println!("Operation:   {}", op.id);
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
