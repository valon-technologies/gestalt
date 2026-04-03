use std::io::{self, BufRead, Write};

use anyhow::{Context, Result};

use crate::api::{self, PROJECT_CONFIG_FILE};
use crate::commands::auth;
use crate::config::ConfigStore;
use crate::output;

pub fn run(url_override: Option<&str>) -> Result<()> {
    let stdin = io::stdin();
    let mut lines = stdin.lock().lines();

    eprintln!("Welcome to Gestalt! Let's get you set up.\n");

    let current_url = api::resolve_url(url_override).unwrap_or_default();
    let url = api::normalize_url(&prompt(&mut lines, "API server URL", &current_url)?);

    let store = ConfigStore::new()?;
    store.set("url", &url)?;
    eprintln!("Saved to global config.\n");

    if confirm(&mut lines, "Log in now?", true)? {
        eprintln!();
        auth::login(Some(&url))?;
        eprintln!();
    }

    if confirm(
        &mut lines,
        "Create .gestalt.json in current directory?",
        false,
    )? {
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

fn prompt(lines: &mut io::Lines<io::StdinLock>, label: &str, default: &str) -> Result<String> {
    if default.is_empty() {
        eprint!("{}: ", label);
    } else {
        eprint!("{} [{}]: ", label, default);
    }
    io::stderr().flush()?;

    let line = lines
        .next()
        .unwrap_or(Ok(String::new()))
        .context("failed to read input")?;
    let trimmed = line.trim();

    if trimmed.is_empty() {
        Ok(default.to_string())
    } else {
        Ok(trimmed.to_string())
    }
}

fn confirm(lines: &mut io::Lines<io::StdinLock>, question: &str, default: bool) -> Result<bool> {
    let hint = if default { "Y/n" } else { "y/N" };
    eprint!("{} [{}]: ", question, hint);
    io::stderr().flush()?;

    let line = lines
        .next()
        .unwrap_or(Ok(String::new()))
        .context("failed to read input")?;
    let trimmed = line.trim().to_lowercase();

    if trimmed.is_empty() {
        Ok(default)
    } else {
        Ok(trimmed.starts_with('y'))
    }
}
