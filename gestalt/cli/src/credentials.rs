use std::collections::BTreeMap;
use std::fs;
use std::path::PathBuf;

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::config::ConfigStore;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct Credentials {
    pub api_token: String,
    pub api_token_id: String,
}

#[derive(Debug, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct CredentialsFile {
    #[serde(default)]
    pub servers: BTreeMap<String, Credentials>,
}

pub struct CredentialStore {
    path: PathBuf,
}

enum RawCredentialsFile {
    New(CredentialsFile),
    Legacy(Credentials),
}

impl CredentialStore {
    pub fn new() -> Result<Self> {
        let config_dir = crate::paths::gestalt_config_dir()?;
        Ok(Self {
            path: config_dir.join("credentials.json"),
        })
    }

    pub fn load_for_origin(&self, origin: &str) -> Result<Option<Credentials>> {
        let origin = normalize_origin(origin);
        let Some(raw) = self.read_raw()? else {
            return Ok(None);
        };
        Ok(match raw {
            RawCredentialsFile::New(file) => file
                .servers
                .get(&origin)
                .filter(|creds| !creds.api_token.trim().is_empty())
                .cloned(),
            RawCredentialsFile::Legacy(legacy) => Self::legacy_for_origin(&legacy, &origin),
        })
    }

    pub fn save_for_origin(&self, origin: &str, credentials: &Credentials) -> Result<()> {
        let origin = normalize_origin(origin);
        let mut file = match self.read_raw()? {
            None => CredentialsFile::default(),
            Some(raw) => match raw {
                RawCredentialsFile::New(file) => file,
                RawCredentialsFile::Legacy(legacy) => self.migrate_legacy_credentials(legacy)?,
            },
        };
        file.servers.insert(origin, credentials.clone());
        self.write_credentials_file(&file)
    }

    pub fn delete_for_origin(&self, origin: &str) -> Result<()> {
        let origin = normalize_origin(origin);
        let Some(raw) = self.read_raw()? else {
            return Ok(());
        };
        match raw {
            RawCredentialsFile::New(mut file) => {
                if file.servers.is_empty() {
                    return Ok(());
                }
                file.servers.remove(&origin);
                if file.servers.is_empty() {
                    self.delete_file()
                } else {
                    self.write_credentials_file(&file)
                }
            }
            RawCredentialsFile::Legacy(legacy) => {
                if Self::legacy_for_origin(&legacy, &origin).is_some() {
                    self.delete_file()
                } else {
                    Ok(())
                }
            }
        }
    }

    pub fn has_any_stored_credentials(&self) -> Result<bool> {
        let Some(raw) = self.read_raw()? else {
            return Ok(false);
        };
        Ok(match raw {
            RawCredentialsFile::New(file) => file
                .servers
                .values()
                .any(|creds| !creds.api_token.trim().is_empty()),
            RawCredentialsFile::Legacy(_) => false,
        })
    }

    fn legacy_for_origin(legacy: &Credentials, origin: &str) -> Option<Credentials> {
        if legacy.api_token.trim().is_empty() {
            return None;
        }
        let url = Self::configured_origin_url()?;
        if normalize_origin(&url) == origin {
            Some(legacy.clone())
        } else {
            None
        }
    }

    fn migrate_legacy_credentials(&self, legacy: Credentials) -> Result<CredentialsFile> {
        if legacy.api_token.trim().is_empty() {
            return Err(anyhow::anyhow!(
                "failed to parse credentials file: unrecognized credentials file format"
            ));
        }
        let Some(url) = Self::configured_origin_url() else {
            return Ok(CredentialsFile::default());
        };
        let origin = normalize_origin(&url);
        let mut file = CredentialsFile::default();
        file.servers.insert(origin, legacy);
        self.write_credentials_file(&file)?;
        Ok(file)
    }

    fn configured_origin_url() -> Option<String> {
        let store = ConfigStore::new().ok()?;
        store.get("url").ok().flatten()
    }

    pub fn take_legacy_credentials_without_config_url(&self) -> Result<Option<Credentials>> {
        if Self::configured_origin_url().is_some() {
            return Ok(None);
        }
        Ok(match self.read_raw()? {
            Some(RawCredentialsFile::Legacy(legacy)) => Some(legacy),
            _ => None,
        })
    }

    fn read_raw(&self) -> Result<Option<RawCredentialsFile>> {
        let json = match fs::read_to_string(&self.path) {
            Ok(json) => json,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
            Err(e) => return Err(anyhow::anyhow!(e).context("failed to read credentials file")),
        };

        let value: Value =
            serde_json::from_str(&json).context("failed to parse credentials file")?;
        if value.get("servers").is_some() {
            let file: CredentialsFile =
                serde_json::from_value(value).context("failed to parse credentials file")?;
            return Ok(Some(RawCredentialsFile::New(file)));
        }

        let creds: Credentials =
            serde_json::from_str(&json).context("failed to parse credentials file")?;
        if creds.api_token.trim().is_empty() {
            return Err(anyhow::anyhow!(
                "failed to parse credentials file: unrecognized credentials file format"
            ));
        }
        Ok(Some(RawCredentialsFile::Legacy(creds)))
    }

    fn write_credentials_file(&self, file: &CredentialsFile) -> Result<()> {
        if let Some(parent) = self.path.parent() {
            fs::create_dir_all(parent).context("failed to create config directory")?;
        }

        let json = serde_json::to_string_pretty(file).context("failed to serialize credentials")?;
        write_secure(&self.path, json.as_bytes())?;
        Ok(())
    }

    pub fn delete_file(&self) -> Result<()> {
        match fs::remove_file(&self.path) {
            Ok(()) => Ok(()),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(e) => Err(anyhow::anyhow!(e).context("failed to delete credentials file")),
        }
    }
}

pub fn normalize_origin(origin: &str) -> String {
    let normalized = crate::api::normalize_url(origin);
    match url::Url::parse(&normalized) {
        Ok(parsed) => {
            let scheme = parsed.scheme().to_ascii_lowercase();
            let Some(host) = parsed.host_str() else {
                return normalized;
            };
            let host = host.to_ascii_lowercase();
            let authority = match parsed.port() {
                Some(port) if !is_default_origin_port(&scheme, port) => format!("{host}:{port}"),
                _ => host,
            };
            let out = format!("{scheme}://{authority}");
            out
        }
        Err(_) => normalized,
    }
}

fn is_default_origin_port(scheme: &str, port: u16) -> bool {
    matches!((scheme, port), ("https", 443) | ("http", 80))
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
