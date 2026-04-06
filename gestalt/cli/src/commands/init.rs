use anyhow::{Context, Result};

use crate::api::{self, PROJECT_CONFIG_FILE};
use crate::commands::auth;
use crate::config::ConfigStore;
use crate::interactive::{InputPrompt, prompt_confirm, prompt_input};
use crate::output;

pub fn run(url_override: Option<&str>) -> Result<()> {
    eprintln!("Welcome to Gestalt! Let's get you set up.\n");

    let current_url = api::resolve_url(url_override).unwrap_or_default();
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

    if prompt_confirm("Log in now?", true)? {
        eprintln!();
        auth::login(Some(&url))?;
        eprintln!();
    }

    if prompt_confirm("Create .gestalt.json in current directory?", false)? {
        let config = serde_json::json!({"url": url});
        let json = serde_json::to_string_pretty(&config).context("failed to serialize config")?;
        std::fs::write(PROJECT_CONFIG_FILE, format!("{json}\n"))
            .context("failed to write .gestalt.json")?;
        output::print_success(&format!("Created {}", PROJECT_CONFIG_FILE));
    }

    eprintln!();
    output::print_success("You're all set! Run 'gestalt --help' to see available commands.");
    Ok(())
}
