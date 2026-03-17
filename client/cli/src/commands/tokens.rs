use anyhow::{Context, Result};

use crate::api::ApiClient;
use crate::output::{self, Format};

pub fn create(url_override: Option<&str>, name: Option<&str>, format: Format) -> Result<()> {
    let client = ApiClient::from_env(url_override)?;
    let token_name = name.unwrap_or("cli-token");
    let body = serde_json::json!({"name": token_name});
    let resp = client
        .post("/api/v1/tokens", &body)
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

pub fn list(url_override: Option<&str>, format: Format) -> Result<()> {
    let client = ApiClient::from_env(url_override)?;
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
                    ]
                })
                .collect();
            output::print_table(&["ID", "Name", "Scopes", "Created"], &rows);
        }
    }

    Ok(())
}

pub fn revoke(url_override: Option<&str>, id: &str, format: Format) -> Result<()> {
    let client = ApiClient::from_env(url_override)?;
    let path = format!("/api/v1/tokens/{}", id);
    let resp = client.delete(&path).context("failed to revoke token")?;

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => output::print_success(&format!("Token {} revoked.", id)),
    }

    Ok(())
}
