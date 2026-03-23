use anyhow::{Context, Result};

use crate::api::ApiClient;
use crate::output::{self, Format};

pub fn list(url_override: Option<&str>, format: Format) -> Result<()> {
    let client = ApiClient::from_env(url_override)?;
    let resp = client
        .get("/api/v1/integrations")
        .context("failed to list integrations")?;

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            let items = resp.as_array().unwrap_or(&Vec::new()).clone();
            let rows: Vec<Vec<String>> = items
                .iter()
                .map(|item| {
                    let connected = match item["connected"].as_bool() {
                        Some(true) => "yes",
                        _ => "no",
                    };
                    vec![
                        item["name"].as_str().unwrap_or("-").to_string(),
                        item["display_name"].as_str().unwrap_or("-").to_string(),
                        item["auth_type"].as_str().unwrap_or("-").to_string(),
                        connected.into(),
                        item["description"].as_str().unwrap_or("-").to_string(),
                    ]
                })
                .collect();
            output::print_table(
                &[
                    "Name",
                    "Display Name",
                    "Auth Type",
                    "Connected",
                    "Description",
                ],
                &rows,
            );
        }
    }
    Ok(())
}

pub fn connect(url_override: Option<&str>, name: &str) -> Result<()> {
    let client = ApiClient::from_env(url_override)?;
    let body = serde_json::json!({"integration": name});
    let resp = client
        .post("/api/v1/auth/start-oauth", &body)
        .context("failed to start OAuth flow")?;

    let url = resp["url"]
        .as_str()
        .context("response missing 'url' field")?;

    eprintln!("Opening browser to connect {}...", name);
    eprintln!("If the browser doesn't open, visit: {}", url);

    if open::that(url).is_err() {
        eprintln!("Could not open browser automatically.");
    }

    Ok(())
}
