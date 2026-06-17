use anyhow::{Context, Result};

use crate::api::ApiClient;
use crate::output::{self, Format};

pub fn create(client: &ApiClient, name: Option<&str>, scopes: &str, format: Format) -> Result<()> {
    let token_name = name.unwrap_or("cli-token");
    let resp = client
        .create_api_token(token_name, scopes)
        .context("failed to create token")?;

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            if let Some(token) = resp["token"].as_str() {
                output::print_success("Token created. Save it now; it won't be shown again.");
                println!("{}", token);
            } else {
                output::print_json(&resp);
            }
        }
    }

    Ok(())
}

fn token_scopes_display(item: &serde_json::Value) -> String {
    if let Some(scopes) = item.get("scopes").and_then(|v| v.as_array()) {
        let parts: Vec<String> = scopes
            .iter()
            .filter_map(|scope| scope.as_str().map(str::to_string))
            .collect();
        if parts.is_empty() {
            return "-".to_string();
        }
        return parts.join(" ");
    }
    "-".to_string()
}

fn token_timestamp_display(item: &serde_json::Value, field: &str, missing: &str) -> String {
    item.get(field)
        .and_then(|v| v.as_str())
        .filter(|value| !value.is_empty())
        .unwrap_or(missing)
        .to_string()
}

pub fn list(client: &ApiClient, format: Format) -> Result<()> {
    let resp = client
        .get("/api/v1/tokens")
        .context("failed to list tokens")?;

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            let items = resp.as_array().unwrap_or(&Vec::new()).clone();
            let rows: Vec<Vec<String>> = items
                .iter()
                .map(|item| {
                    vec![
                        item["id"].as_str().unwrap_or("-").to_string(),
                        token_scopes_display(item),
                        token_timestamp_display(item, "createdAt", "-"),
                        token_timestamp_display(item, "expiresAt", "never"),
                    ]
                })
                .collect();
            output::print_table(&["ID", "Scopes", "Created", "Expires"], &rows);
        }
    }

    Ok(())
}

pub fn revoke(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let resp = client
        .revoke_api_token(id)
        .context("failed to revoke token")?;

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => output::print_success(&format!("Token {} revoked.", id)),
    }

    Ok(())
}
