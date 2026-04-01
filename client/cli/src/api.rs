use anyhow::{Context, Result, bail};
use reqwest::StatusCode;
use reqwest::blocking::Client;
use reqwest::header::{self, HeaderValue};

use crate::config::ConfigStore;
use crate::credentials::{CredentialStore, StoredTokenKind};

pub const DEFAULT_URL: &str = "http://localhost:8080";
pub const ENV_API_KEY: &str = "GESTALT_API_KEY";

pub fn normalize_url(url: &str) -> String {
    let trimmed = url.trim().trim_end_matches('/');
    if trimmed.contains("://") {
        trimmed.to_string()
    } else {
        format!("https://{trimmed}")
    }
}

pub fn resolve_url(url_override: Option<&str>) -> Result<String> {
    if let Some(url) = url_override {
        return Ok(url.to_string());
    }
    if let Ok(url) = std::env::var("GESTALT_URL") {
        return Ok(url);
    }
    if let Some(url) = find_project_config_value("url") {
        return Ok(url);
    }
    if let Ok(Some(url)) = ConfigStore::new().and_then(|s| s.get("url")) {
        return Ok(url);
    }
    if let Ok(Some(creds)) = CredentialStore::new().and_then(|s| s.load()) {
        return Ok(creds.api_url);
    }
    Ok(DEFAULT_URL.to_string())
}

pub const PROJECT_CONFIG_FILE: &str = ".gestalt.json";

fn find_project_config_value(key: &str) -> Option<String> {
    let mut dir = std::env::current_dir().ok()?;
    loop {
        let candidate = dir.join(PROJECT_CONFIG_FILE);
        if candidate.is_file() {
            let contents = std::fs::read_to_string(&candidate).ok()?;
            let map: std::collections::BTreeMap<String, String> =
                serde_json::from_str(&contents).ok()?;
            return map.get(key).cloned();
        }
        if !dir.pop() {
            return None;
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TokenSource {
    EnvVar,
    ApiToken,
    Session,
}

impl From<StoredTokenKind> for TokenSource {
    fn from(value: StoredTokenKind) -> Self {
        match value {
            StoredTokenKind::ApiToken => TokenSource::ApiToken,
            StoredTokenKind::SessionToken => TokenSource::Session,
        }
    }
}

pub fn env_api_key_is_set() -> bool {
    std::env::var(ENV_API_KEY)
        .map(|v| !v.is_empty())
        .unwrap_or(false)
}

pub struct ApiClient {
    client: Client,
    base_url: String,
    token: String,
    token_source: TokenSource,
}

impl ApiClient {
    pub fn from_env(url_override: Option<&str>) -> Result<Self> {
        let (token, source) =
            if let Some(key) = std::env::var(ENV_API_KEY).ok().filter(|v| !v.is_empty()) {
                (key, TokenSource::EnvVar)
            } else {
                let store = CredentialStore::new()?;
                match store.load()? {
                    Some(creds) => match creds.stored_token() {
                        Some((token, kind)) => (token.to_string(), kind.into()),
                        None => {
                            bail!(
                                "not authenticated: set {} or run 'gestalt auth login'",
                                ENV_API_KEY
                            )
                        }
                    },
                    None => {
                        bail!(
                            "not authenticated: set {} or run 'gestalt auth login'",
                            ENV_API_KEY
                        )
                    }
                }
            };

        let base_url = resolve_url(url_override)?;
        let mut client = Self::new(&base_url, &token)?;
        client.token_source = source;
        Ok(client)
    }

    pub fn new(base_url: &str, token: &str) -> Result<Self> {
        let mut default_headers = header::HeaderMap::new();
        default_headers.insert(header::ACCEPT, HeaderValue::from_static("application/json"));

        let client = Client::builder()
            .timeout(std::time::Duration::from_secs(30))
            .default_headers(default_headers)
            .build()
            .context("failed to build HTTP client")?;

        Ok(Self {
            client,
            base_url: base_url.trim_end_matches('/').to_string(),
            token: token.to_string(),
            token_source: TokenSource::Session,
        })
    }

    pub fn get(&self, path: &str) -> Result<serde_json::Value> {
        let url = format!("{}{}", self.base_url, path);
        let resp = self
            .client
            .get(&url)
            .bearer_auth(&self.token)
            .send()
            .with_context(|| format!("request to {} failed", url))?;

        self.handle_response(resp)
    }

    pub fn post(&self, path: &str, body: &serde_json::Value) -> Result<serde_json::Value> {
        let url = format!("{}{}", self.base_url, path);
        let resp = self
            .client
            .post(&url)
            .bearer_auth(&self.token)
            .json(body)
            .send()
            .with_context(|| format!("request to {} failed", url))?;

        self.handle_response(resp)
    }

    pub fn delete(&self, path: &str) -> Result<serde_json::Value> {
        let url = format!("{}{}", self.base_url, path);
        let resp = self
            .client
            .delete(&url)
            .bearer_auth(&self.token)
            .send()
            .with_context(|| format!("request to {} failed", url))?;

        self.handle_response(resp)
    }

    fn handle_response(&self, resp: reqwest::blocking::Response) -> Result<serde_json::Value> {
        let status = resp.status();

        if status == StatusCode::NO_CONTENT {
            return Ok(serde_json::json!({"status": "ok"}));
        }

        let body = resp.text().context("failed to read response body")?;

        if status.is_client_error() || status.is_server_error() {
            let message = serde_json::from_str::<serde_json::Value>(&body)
                .ok()
                .and_then(|v| extract_error_message(&v))
                .unwrap_or_else(|| format!("HTTP {}: {}", status.as_u16(), body));

            if status == StatusCode::UNAUTHORIZED && self.token_source == TokenSource::EnvVar {
                bail!(
                    "{} (using {} from environment; \
                     unset it to use your session token from 'gestalt auth login')",
                    message,
                    ENV_API_KEY,
                );
            }
            if status == StatusCode::UNAUTHORIZED && self.token_source == TokenSource::ApiToken {
                bail!(
                    "{} (stored CLI API token may be expired or revoked; run 'gestalt auth login' to mint a new one)",
                    message,
                );
            }
            if status == StatusCode::UNAUTHORIZED && self.token_source == TokenSource::Session {
                bail!(
                    "{} (stored session token may be expired; run 'gestalt auth login')",
                    message,
                );
            }

            bail!("{}", message);
        }

        if body.is_empty() {
            return Ok(serde_json::json!({}));
        }

        serde_json::from_str(&body).context("failed to parse response JSON")
    }
}

fn extract_error_message(value: &serde_json::Value) -> Option<String> {
    match value {
        serde_json::Value::Object(obj) => {
            if let Some(message) = obj.get("error").and_then(|err| match err {
                serde_json::Value::String(message) => Some(message.clone()),
                serde_json::Value::Object(err_obj) => err_obj
                    .get("message")
                    .and_then(|message| message.as_str())
                    .map(String::from),
                _ => None,
            }) {
                return Some(message);
            }

            obj.get("message")
                .and_then(|message| message.as_str())
                .map(String::from)
        }
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_client_creation() {
        let client = ApiClient::new("http://localhost:8080", "test-token");
        assert!(client.is_ok());
    }

    #[test]
    fn test_base_url_trailing_slash() {
        let client = ApiClient::new("http://localhost:8080/", "test-token").unwrap();
        assert_eq!(client.base_url, "http://localhost:8080");
    }

    #[test]
    fn test_extract_error_message_string_error() {
        let value = serde_json::json!({"error": "plain error"});
        assert_eq!(
            extract_error_message(&value).as_deref(),
            Some("plain error")
        );
    }

    #[test]
    fn test_extract_error_message_nested_error_message() {
        let value = serde_json::json!({"error": {"message": "nested error"}});
        assert_eq!(
            extract_error_message(&value).as_deref(),
            Some("nested error")
        );
    }
}
