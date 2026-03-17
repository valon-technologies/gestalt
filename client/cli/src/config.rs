use std::collections::BTreeMap;
use std::fs;
use std::path::PathBuf;

use anyhow::{Context, Result};

pub struct ConfigStore {
    path: PathBuf,
}

impl ConfigStore {
    pub fn new() -> Result<Self> {
        let config_dir = dirs::config_dir()
            .context("could not determine config directory")?
            .join("toolshed");
        Ok(Self {
            path: config_dir.join("config.json"),
        })
    }

    #[cfg(test)]
    pub fn with_path(path: PathBuf) -> Self {
        Self { path }
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_set_and_get() {
        let dir = tempfile::tempdir().unwrap();
        let store = ConfigStore::with_path(dir.path().join("config.json"));

        store.set("url", "https://example.com").unwrap();
        assert_eq!(
            store.get("url").unwrap(),
            Some("https://example.com".to_string())
        );
    }

    #[test]
    fn test_get_missing_key() {
        let dir = tempfile::tempdir().unwrap();
        let store = ConfigStore::with_path(dir.path().join("config.json"));
        assert_eq!(store.get("url").unwrap(), None);
    }

    #[test]
    fn test_remove() {
        let dir = tempfile::tempdir().unwrap();
        let store = ConfigStore::with_path(dir.path().join("config.json"));

        store.set("url", "https://example.com").unwrap();
        store.remove("url").unwrap();
        assert_eq!(store.get("url").unwrap(), None);
    }

    #[test]
    fn test_list() {
        let dir = tempfile::tempdir().unwrap();
        let store = ConfigStore::with_path(dir.path().join("config.json"));

        store.set("url", "https://example.com").unwrap();
        store.set("other", "value").unwrap();
        let map = store.load().unwrap();
        assert_eq!(map.len(), 2);
    }
}
