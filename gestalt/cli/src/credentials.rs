use std::fs;
use std::path::PathBuf;

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};

const KEYCHAIN_SERVICE: &str = "gestalt";
const KEYCHAIN_USER: &str = "credentials";

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Credentials {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub api_url: Option<String>,
    pub api_token: String,
    pub api_token_id: String,
}

impl Credentials {
    pub fn api_url(&self) -> Option<&str> {
        self.api_url
            .as_deref()
            .map(str::trim)
            .filter(|url| !url.is_empty())
    }
}

pub struct CredentialStore {
    path: PathBuf,
}

impl CredentialStore {
    pub fn new() -> Result<Self> {
        let config_dir = crate::paths::gestalt_config_dir()?;
        Ok(Self {
            path: config_dir.join("credentials.json"),
        })
    }

    pub fn load(&self) -> Result<Option<Credentials>> {
        if let Some(creds) = load_from_keychain() {
            return Ok(Some(creds));
        }
        self.load_from_file()
    }

    pub fn save(&self, credentials: &Credentials) -> Result<()> {
        if save_to_keychain(credentials) {
            let _ = fs::remove_file(&self.path);
            return Ok(());
        }
        self.save_to_file(credentials)
    }

    pub fn delete(&self) -> Result<()> {
        delete_from_keychain();
        match fs::remove_file(&self.path) {
            Ok(()) => Ok(()),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(e) => Err(anyhow::anyhow!(e).context("failed to delete credentials file")),
        }
    }

    fn load_from_file(&self) -> Result<Option<Credentials>> {
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

    fn save_to_file(&self, credentials: &Credentials) -> Result<()> {
        if let Some(parent) = self.path.parent() {
            fs::create_dir_all(parent).context("failed to create config directory")?;
        }
        let json =
            serde_json::to_string_pretty(credentials).context("failed to serialize credentials")?;
        write_secure(&self.path, json.as_bytes())?;
        Ok(())
    }
}

fn keychain_entry() -> Option<keyring::Entry> {
    keyring::Entry::new(KEYCHAIN_SERVICE, KEYCHAIN_USER).ok()
}

fn load_from_keychain() -> Option<Credentials> {
    let entry = keychain_entry()?;
    let json = entry.get_password().ok()?;
    serde_json::from_str(&json).ok()
}

fn save_to_keychain(credentials: &Credentials) -> bool {
    let Some(entry) = keychain_entry() else {
        return false;
    };
    let Ok(json) = serde_json::to_string(credentials) else {
        return false;
    };
    entry.set_password(&json).is_ok()
}

fn delete_from_keychain() {
    if let Some(entry) = keychain_entry() {
        let _ = entry.delete_credential();
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
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600))
        .context("failed to set file permissions to 0600")
}

#[cfg(not(unix))]
fn write_secure(path: &std::path::Path, data: &[u8]) -> Result<()> {
    std::fs::write(path, data).context("failed to write credentials file")
}
