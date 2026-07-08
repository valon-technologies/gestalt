use anyhow::Result;

use crate::api::{self, DEFAULT_URL};
use crate::commands::auth;
use crate::config::ConfigStore;
use crate::interactive::{InputPrompt, prompt_confirm, prompt_input};
use crate::output;

pub fn run(url_override: Option<&str>) -> Result<()> {
    eprintln!("Welcome to Gestalt! Let's get you set up.\n");

    let current_url = api::resolve_url(url_override).unwrap_or_else(|_| DEFAULT_URL.to_string());
    let url = api::normalize_url(&prompt_input(&InputPrompt {
        label: "API server URL".to_string(),
        description: None,
        default: Some(current_url),
        required: true,
        secret: false,
    })?);

    let store = ConfigStore::new()?;
    store.set("url", &url)?;
    eprintln!("Saved to global config.\n");

    if api::server_auth_disabled(&url).unwrap_or(false) {
        eprintln!("Authentication is disabled on this server; skipping login.\n");
    } else if prompt_confirm("Log in now?", true)? {
        eprintln!();
        auth::login(Some(&url))?;
        eprintln!();
    }

    eprintln!();
    output::print_success("You're all set! Run 'gestalt --help' to see available commands.");
    Ok(())
}
