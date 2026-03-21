use std::fs;
use std::path::PathBuf;

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Credentials {
    pub api_url: String,
    pub session_token: String,
}

pub struct CredentialStore {
    path: PathBuf,
}

impl CredentialStore {
    pub fn new() -> Result<Self> {
        let config_dir = dirs::config_dir()
            .context("could not determine config directory")?
            .join("gestalt");
        Ok(Self {
            path: config_dir.join("credentials.json"),
        })
    }

    #[cfg(test)]
    pub fn with_path(path: PathBuf) -> Self {
        Self { path }
    }

    pub fn load(&self) -> Result<Option<Credentials>> {
        if !self.path.exists() {
            return Ok(None);
        }
        let json = fs::read_to_string(&self.path).context("failed to read credentials file")?;
        let creds: Credentials =
            serde_json::from_str(&json).context("failed to parse credentials file")?;
        Ok(Some(creds))
    }

    pub fn save(&self, credentials: &Credentials) -> Result<()> {
        if let Some(parent) = self.path.parent() {
            fs::create_dir_all(parent).context("failed to create config directory")?;
        }

        let json =
            serde_json::to_string_pretty(credentials).context("failed to serialize credentials")?;
        fs::write(&self.path, &json).context("failed to write credentials file")?;

        set_permissions(&self.path)?;
        Ok(())
    }

    pub fn delete(&self) -> Result<()> {
        if self.path.exists() {
            fs::remove_file(&self.path).context("failed to delete credentials file")?;
        }
        Ok(())
    }
}

#[cfg(unix)]
fn set_permissions(path: &std::path::Path) -> Result<()> {
    use std::os::unix::fs::PermissionsExt;
    fs::set_permissions(path, fs::Permissions::from_mode(0o600))
        .context("failed to set file permissions to 0600")?;
    Ok(())
}

#[cfg(not(unix))]
fn set_permissions(_path: &std::path::Path) -> Result<()> {
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_save_and_load() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("credentials.json");
        let store = CredentialStore::with_path(path.clone());

        let creds = Credentials {
            api_url: "http://localhost:8080".to_string(),
            session_token: "test-token".to_string(),
        };

        store.save(&creds).unwrap();
        let loaded = store.load().unwrap().unwrap();
        assert_eq!(loaded.session_token, "test-token");
        assert_eq!(loaded.api_url, "http://localhost:8080");
    }

    #[test]
    fn test_load_nonexistent() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("does-not-exist.json");
        let store = CredentialStore::with_path(path);

        assert!(store.load().unwrap().is_none());
    }

    #[test]
    fn test_delete() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("credentials.json");
        let store = CredentialStore::with_path(path.clone());

        let creds = Credentials {
            api_url: "http://localhost:8080".to_string(),
            session_token: "test-token".to_string(),
        };

        store.save(&creds).unwrap();
        assert!(path.exists());

        store.delete().unwrap();
        assert!(!path.exists());
    }

    #[cfg(unix)]
    #[test]
    fn test_permissions() {
        use std::os::unix::fs::PermissionsExt;

        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("credentials.json");
        let store = CredentialStore::with_path(path.clone());

        let creds = Credentials {
            api_url: "http://localhost:8080".to_string(),
            session_token: "test-token".to_string(),
        };

        store.save(&creds).unwrap();
        let metadata = std::fs::metadata(&path).unwrap();
        let mode = metadata.permissions().mode() & 0o777;
        assert_eq!(mode, 0o600);
    }
}
