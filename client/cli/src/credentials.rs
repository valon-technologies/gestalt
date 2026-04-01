use std::fs;
use std::path::PathBuf;

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Credentials {
    pub api_url: String,
    pub api_token: String,
    pub api_token_id: String,
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

    pub fn exists(&self) -> Result<bool> {
        match fs::metadata(&self.path) {
            Ok(_) => Ok(true),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(false),
            Err(e) => Err(anyhow::anyhow!(e).context("failed to stat credentials file")),
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
