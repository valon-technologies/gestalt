use anyhow::{Context, Result};
use serde::Serialize;

use crate::api::ApiClient;
use crate::output::{self, Format};

#[derive(Serialize)]
struct StartOAuthRequest<'a> {
    integration: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    connection: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    instance: Option<&'a str>,
}

pub fn list(client: &ApiClient, format: Format) -> Result<()> {
    let resp = client
        .get("/api/v1/integrations")
        .context("failed to list integrations")?;

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            let rows: Vec<Vec<String>> = resp
                .as_array()
                .unwrap_or(&Vec::new())
                .iter()
                .map(|item| {
                    let connected = match item["connected"].as_bool() {
                        Some(true) => "yes",
                        _ => "no",
                    };
                    vec![
                        item["name"].as_str().unwrap_or("-").to_string(),
                        item["description"].as_str().unwrap_or("-").to_string(),
                        connected.into(),
                    ]
                })
                .collect();
            output::print_table(&["Name", "Description", "Connected"], &rows);
        }
    }
    Ok(())
}

pub fn connect(
    client: &ApiClient,
    name: &str,
    connection: Option<&str>,
    instance: Option<&str>,
) -> Result<()> {
    connect_with_browser_opener(client, name, connection, instance, |url| {
        open::that(url).map(|_| ()).map_err(Into::into)
    })
}

pub fn connect_with_browser_opener<F>(
    client: &ApiClient,
    name: &str,
    connection: Option<&str>,
    instance: Option<&str>,
    open_browser: F,
) -> Result<()>
where
    F: FnOnce(&str) -> Result<()>,
{
    let resp = client
        .post(
            "/api/v1/auth/start-oauth",
            &StartOAuthRequest {
                integration: name,
                connection,
                instance,
            },
        )
        .context("failed to start OAuth flow")?;

    let url = resp["url"]
        .as_str()
        .context("response missing 'url' field")?;

    eprintln!("Opening browser to connect {}...", name);
    eprintln!("If the browser doesn't open, visit: {}", url);

    if open_browser(url).is_err() {
        eprintln!("Could not open browser automatically.");
    }

    Ok(())
}
