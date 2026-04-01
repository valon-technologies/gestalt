use anyhow::{Context, Result};

use crate::api::ApiClient;
use crate::output::{self, Format};

pub fn create(client: &ApiClient, name: Option<&str>, format: Format) -> Result<()> {
    let token_name = name.unwrap_or("cli-token");
    let resp = client
        .create_api_token(token_name)
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
                        item["name"].as_str().unwrap_or("-").to_string(),
                        item["scopes"].as_str().unwrap_or("-").to_string(),
                        item["created_at"].as_str().unwrap_or("-").to_string(),
                        item["expires_at"].as_str().unwrap_or("never").to_string(),
                    ]
                })
                .collect();
            output::print_table(&["ID", "Name", "Scopes", "Created", "Expires"], &rows);
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
