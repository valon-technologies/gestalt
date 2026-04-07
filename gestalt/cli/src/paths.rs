use std::path::PathBuf;

use anyhow::{Context, Result};

pub fn gestalt_config_dir() -> Result<PathBuf> {
    if let Ok(xdg) = std::env::var("XDG_CONFIG_HOME") {
        if !xdg.is_empty() {
            return Ok(PathBuf::from(xdg).join("gestalt"));
        }
    }
    let home = dirs::home_dir().context("could not determine home directory")?;
    Ok(home.join(".config").join("gestalt"))
}
