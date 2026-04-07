use anyhow::{Context, Result};
use serde::Deserialize;

use crate::api::{self, AUTH_INFO_PATH, DEFAULT_URL, PROJECT_CONFIG_DIR, PROJECT_CONFIG_FILE};
use crate::commands::auth;
use crate::config::ConfigStore;
use crate::interactive::{InputPrompt, prompt_confirm, prompt_input};
use crate::output;

#[derive(Debug, Deserialize)]
struct AuthInfo {
    login_supported: bool,
}

pub fn run(url_override: Option<&str>) -> Result<()> {
    eprintln!("Welcome to Gestalt! Let's get you set up.\n");

    let current_url = api::resolve_url(url_override).unwrap_or_else(|_| DEFAULT_URL.to_string());
    let url = api::normalize_url(&prompt_input(&InputPrompt {
        label: "API server URL".to_string(),
        description: None,
        help_url: None,
        default: Some(current_url),
        required: true,
        secret: false,
    })?);

    let store = ConfigStore::new()?;
    store.set("url", &url)?;
    eprintln!("Saved to global config.\n");

    if server_auth_disabled(&url) {
        eprintln!("Authentication is disabled on this server; skipping login.\n");
    } else if prompt_confirm("Log in now?", true)? {
        eprintln!();
        auth::login(Some(&url))?;
        eprintln!();
    }

    if prompt_confirm("Create .gestalt/config.json in current directory?", false)? {
        let config = serde_json::json!({"url": url});
        let json = serde_json::to_string_pretty(&config).context("failed to serialize config")?;
        std::fs::create_dir_all(PROJECT_CONFIG_DIR)
            .with_context(|| format!("failed to create {}", PROJECT_CONFIG_DIR))?;
        std::fs::write(PROJECT_CONFIG_FILE, format!("{json}\n"))
            .context("failed to write .gestalt/config.json")?;
        output::print_success(&format!("Created {}", PROJECT_CONFIG_FILE));
    }

    eprintln!();
    output::print_success("You're all set! Run 'gestalt --help' to see available commands.");
    Ok(())
}

fn server_auth_disabled(url: &str) -> bool {
    let auth_info_url = format!("{url}{AUTH_INFO_PATH}");
    let client = reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(5))
        .build()
        .ok();
    let Some(client) = client else {
        return false;
    };
    let resp = client.get(&auth_info_url).send().ok();
    let Some(resp) = resp else {
        return false;
    };
    if !resp.status().is_success() {
        return false;
    }
    resp.json::<AuthInfo>()
        .map(|info| !info.login_supported)
        .unwrap_or(false)
}
