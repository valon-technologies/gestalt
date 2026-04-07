use std::collections::BTreeMap;
use std::fs;
use std::path::Path;
use std::path::PathBuf;

use anyhow::{Context, Result};

pub struct ConfigStore {
    path: PathBuf,
}

impl ConfigStore {
    pub fn new() -> Result<Self> {
        let config_dir = dirs::config_dir()
            .context("could not determine config directory")?
            .join("gestalt");
        Ok(Self {
            path: config_dir.join("config.json"),
        })
    }

    pub fn load(&self) -> Result<BTreeMap<String, String>> {
        if !self.path.exists() {
            return Ok(BTreeMap::new());
        }
        let json = fs::read_to_string(&self.path).context("failed to read config file")?;
        serde_json::from_str(&json).context("failed to parse config file")
    }

    pub fn get(&self, key: &str) -> Result<Option<String>> {
        Ok(self.load()?.get(key).cloned())
    }

    pub fn path(&self) -> &Path {
        &self.path
    }

    pub fn set(&self, key: &str, value: &str) -> Result<()> {
        let mut map = self.load()?;
        map.insert(key.to_string(), value.to_string());
        self.write(&map)
    }

    pub fn remove(&self, key: &str) -> Result<()> {
        let mut map = self.load()?;
        map.remove(key);
        self.write(&map)
    }

    fn write(&self, map: &BTreeMap<String, String>) -> Result<()> {
        if let Some(parent) = self.path.parent() {
            fs::create_dir_all(parent).context("failed to create config directory")?;
        }
        let json = serde_json::to_string_pretty(map).context("failed to serialize config")?;
        fs::write(&self.path, &json).context("failed to write config file")?;
        Ok(())
    }
}
