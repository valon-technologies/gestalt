use anyhow::{bail, Context, Result};
use reqwest::blocking::Client;
use std::time::Duration;

use crate::api;
use crate::output::{self, Format};

const HEALTH_PATH: &str = "/health";
const READY_PATH: &str = "/ready";
const TIMEOUT_SECS: u64 = 10;

pub fn check(url_override: Option<&str>, format: Format) -> Result<()> {
    let base_url = api::resolve_url(url_override)?;
    let client = Client::builder()
        .timeout(Duration::from_secs(TIMEOUT_SECS))
        .build()
        .context("failed to build HTTP client")?;

    let health = fetch_status(&client, &base_url, HEALTH_PATH);
    let ready = fetch_status(&client, &base_url, READY_PATH);

    let healthy = health.as_deref() == Some("ok");
    let ready_ok = ready.as_deref() == Some("ok");

    match format {
        Format::Json => {
            output::print_json(&serde_json::json!({
                "health": health.as_deref().unwrap_or("unreachable"),
                "ready": ready.as_deref().unwrap_or("unreachable"),
            }));
        }
        Format::Table => {
            let rows = vec![
                vec!["health".to_string(), status_display(&health)],
                vec!["ready".to_string(), status_display(&ready)],
            ];
            output::print_table(&["Endpoint", "Status"], &rows);
        }
    }

    if !healthy || !ready_ok {
        bail!("server is not healthy");
    }

    Ok(())
}

fn fetch_status(client: &Client, base_url: &str, path: &str) -> Option<String> {
    let url = format!("{}{}", base_url.trim_end_matches('/'), path);
    let resp = client.get(&url).send().ok()?;
    let body: serde_json::Value = resp.json().ok()?;
    body["status"].as_str().map(String::from)
}

fn status_display(status: &Option<String>) -> String {
    match status.as_deref() {
        Some("ok") => "ok".to_string(),
        Some(other) => other.to_string(),
        None => "unreachable".to_string(),
    }
}
