use std::fs;
use std::path::PathBuf;

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum StoredTokenKind {
    ApiToken,
    SessionToken,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Credentials {
    pub api_url: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub api_token: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub api_token_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub session_token: Option<String>,
}

impl Credentials {
    pub fn stored_token(&self) -> Option<(&str, StoredTokenKind)> {
        self.api_token
            .as_deref()
            .filter(|token| !token.is_empty())
            .map(|token| (token, StoredTokenKind::ApiToken))
            .or_else(|| {
                self.session_token
                    .as_deref()
                    .filter(|token| !token.is_empty())
                    .map(|token| (token, StoredTokenKind::SessionToken))
            })
    }
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
        match fs::read_to_string(&self.path) {
            Ok(json) => {
                let creds: Credentials =
                    serde_json::from_str(&json).context("failed to parse credentials file")?;
                Ok(Some(creds))
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(None),
            Err(e) => Err(anyhow::anyhow!(e).context("failed to read credentials file")),
        }
    }

    pub fn save(&self, credentials: &Credentials) -> Result<()> {
        if let Some(parent) = self.path.parent() {
            fs::create_dir_all(parent).context("failed to create config directory")?;
        }

        let json =
            serde_json::to_string_pretty(credentials).context("failed to serialize credentials")?;
        write_secure(&self.path, json.as_bytes())?;
        Ok(())
    }

    pub fn delete(&self) -> Result<()> {
        match fs::remove_file(&self.path) {
            Ok(()) => Ok(()),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(e) => Err(anyhow::anyhow!(e).context("failed to delete credentials file")),
        }
    }
}

#[cfg(unix)]
fn write_secure(path: &std::path::Path, data: &[u8]) -> Result<()> {
    use std::io::Write;
    use std::os::unix::fs::OpenOptionsExt;
    use std::os::unix::fs::PermissionsExt;
    let mut file = std::fs::OpenOptions::new()
        .write(true)
        .create(true)
        .truncate(true)
        .mode(0o600)
        .open(path)
        .context("failed to create credentials file")?;
    file.write_all(data)
        .context("failed to write credentials file")?;
    // mode() only applies on creation; fix permissions for pre-existing files
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600))
        .context("failed to set file permissions to 0600")
}

#[cfg(not(unix))]
fn write_secure(path: &std::path::Path, data: &[u8]) -> Result<()> {
    std::fs::write(path, data).context("failed to write credentials file")
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
            api_token: Some("gst_api_test-token".to_string()),
            api_token_id: Some("tok-1".to_string()),
            session_token: None,
        };

        store.save(&creds).unwrap();
        let loaded = store.load().unwrap().unwrap();
        assert_eq!(loaded.api_token.as_deref(), Some("gst_api_test-token"));
        assert_eq!(loaded.api_token_id.as_deref(), Some("tok-1"));
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
            api_token: Some("gst_api_test-token".to_string()),
            api_token_id: None,
            session_token: None,
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
            api_token: Some("gst_api_test-token".to_string()),
            api_token_id: None,
            session_token: None,
        };

        store.save(&creds).unwrap();
        let metadata = std::fs::metadata(&path).unwrap();
        let mode = metadata.permissions().mode() & 0o777;
        assert_eq!(mode, 0o600);
    }

    #[test]
    fn test_stored_token_prefers_api_token() {
        let creds = Credentials {
            api_url: "http://localhost:8080".to_string(),
            api_token: Some("gst_api_token".to_string()),
            api_token_id: Some("tok-1".to_string()),
            session_token: Some("session-token".to_string()),
        };

        let (token, kind) = creds.stored_token().unwrap();
        assert_eq!(token, "gst_api_token");
        assert_eq!(kind, StoredTokenKind::ApiToken);
    }

    #[test]
    fn test_load_legacy_session_token_shape() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("credentials.json");
        let store = CredentialStore::with_path(path.clone());

        std::fs::write(
            &path,
            r#"{"api_url":"http://localhost:8080","session_token":"legacy-session-token"}"#,
        )
        .unwrap();

        let loaded = store.load().unwrap().unwrap();
        let (token, kind) = loaded.stored_token().unwrap();
        assert_eq!(token, "legacy-session-token");
        assert_eq!(kind, StoredTokenKind::SessionToken);
    }
}
