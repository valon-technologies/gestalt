use anyhow::{Context, Result};
use serde_json::Value;

use crate::api::ApiClient;
use crate::output::{self, Format};

pub fn list_groups(api: &ApiClient, format: Format) -> Result<()> {
    let resp = api
        .get("/api/v1/groups")
        .context("failed to list workspace groups")?;
    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            let rows: Vec<Vec<String>> = resp
                .as_array()
                .unwrap_or(&Vec::new())
                .iter()
                .map(group_row)
                .collect();
            println!(
                "{}",
                output::render_table(
                    &[
                        "ID",
                        "Display Name",
                        "Members",
                        "SCIM Managed",
                        "Editable",
                    ],
                    &rows,
                )
            );
        }
    }
    Ok(())
}

fn group_row(value: &Value) -> Vec<String> {
    vec![
        value
            .get("id")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string(),
        value
            .get("displayName")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string(),
        value
            .get("memberCount")
            .and_then(Value::as_u64)
            .map(|count| count.to_string())
            .unwrap_or_default(),
        value
            .get("scimManaged")
            .and_then(Value::as_bool)
            .map(|managed| managed.to_string())
            .unwrap_or_default(),
        value
            .get("editable")
            .and_then(Value::as_bool)
            .map(|editable| editable.to_string())
            .unwrap_or_default(),
    ]
}
