use anyhow::{bail, Result};

use crate::config::ConfigStore;
use crate::output::{self, Format};

const VALID_KEYS: &[&str] = &["url"];

fn validate_key(key: &str) -> Result<()> {
    if !VALID_KEYS.contains(&key) {
        bail!(
            "unknown config key '{}'. Valid keys: {}",
            key,
            VALID_KEYS.join(", ")
        );
    }
    Ok(())
}

pub fn get(key: &str, format: Format) -> Result<()> {
    validate_key(key)?;
    let store = ConfigStore::new()?;
    match store.get(key)? {
        Some(value) => match format {
            Format::Json => output::print_json(&serde_json::json!({key: value})),
            Format::Table => println!("{}", value),
        },
        None => match format {
            Format::Json => output::print_json(&serde_json::json!({key: null})),
            Format::Table => eprintln!("{} is not set", key),
        },
    }
    Ok(())
}

pub fn set(key: &str, value: &str) -> Result<()> {
    validate_key(key)?;
    let normalized = if key == "url" {
        crate::api::normalize_url(value)
    } else {
        value.to_string()
    };
    let store = ConfigStore::new()?;
    store.set(key, &normalized)?;
    output::print_success(&format!("{} = {}", key, normalized));
    Ok(())
}

pub fn unset(key: &str) -> Result<()> {
    validate_key(key)?;
    let store = ConfigStore::new()?;
    store.remove(key)?;
    output::print_success(&format!("{} removed", key));
    Ok(())
}

pub fn list(format: Format) -> Result<()> {
    let store = ConfigStore::new()?;
    let map = store.load()?;

    match format {
        Format::Json => {
            output::print_json(&serde_json::to_value(&map)?);
        }
        Format::Table => {
            if map.is_empty() {
                println!("No configuration set.");
            } else {
                let rows: Vec<Vec<String>> = map
                    .iter()
                    .map(|(k, v)| vec![k.clone(), v.clone()])
                    .collect();
                output::print_table(&["Key", "Value"], &rows);
            }
        }
    }
    Ok(())
}
