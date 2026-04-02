use std::cell::RefCell;

use anyhow::{Context, Result, bail};
use reqwest::Method;
use reqwest::StatusCode;
use reqwest::blocking::Client;
use reqwest::header::{self, HeaderValue};

use crate::config::ConfigStore;
use crate::credentials::{CredentialStore, Credentials};

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
    Direct,
    EnvVar,
    StoredCredentials,
}

struct AuthState {
    token_source: TokenSource,
    access_token: String,
    stored_credentials: Option<Credentials>,
}

pub fn env_api_key_is_set() -> bool {
    std::env::var(ENV_API_KEY)
        .map(|v| !v.is_empty())
        .unwrap_or(false)
}

pub struct ApiClient {
    client: Client,
    base_url: String,
    auth: RefCell<AuthState>,
}

impl ApiClient {
    pub fn from_env(url_override: Option<&str>) -> Result<Self> {
        let base_url = resolve_url(url_override)?;
        if let Some(key) = std::env::var(ENV_API_KEY).ok().filter(|v| !v.is_empty()) {
            return Self::new_with_source(&base_url, &key, TokenSource::EnvVar);
        }

        match CredentialStore::new()?.load()? {
            Some(creds) => Self::from_credentials(&base_url, creds),
            None => bail!(
                "not authenticated: set {} or run 'gestalt auth login'",
                ENV_API_KEY
            ),
        }
    }

    pub fn from_credentials(base_url: &str, credentials: Credentials) -> Result<Self> {
        let access_token = credentials.access_token.clone();
        Self::build(
            base_url,
            AuthState {
                token_source: TokenSource::StoredCredentials,
                access_token,
                stored_credentials: Some(credentials),
            },
        )
    }

    pub fn new(base_url: &str, token: &str) -> Result<Self> {
        Self::new_with_source(base_url, token, TokenSource::Direct)
    }

    fn new_with_source(base_url: &str, token: &str, token_source: TokenSource) -> Result<Self> {
        Self::build(
            base_url,
            AuthState {
                token_source,
                access_token: token.to_string(),
                stored_credentials: None,
            },
        )
    }

    fn build(base_url: &str, auth: AuthState) -> Result<Self> {
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
            auth: RefCell::new(auth),
        })
    }

    pub fn get(&self, path: &str) -> Result<serde_json::Value> {
        self.send_json_request(Method::GET, path, None)
    }

    pub fn post(&self, path: &str, body: &serde_json::Value) -> Result<serde_json::Value> {
        self.send_json_request(Method::POST, path, Some(body))
    }

    pub fn delete(&self, path: &str) -> Result<serde_json::Value> {
        self.send_json_request(Method::DELETE, path, None)
    }

    pub fn create_api_token(&self, name: &str) -> Result<serde_json::Value> {
        self.post("/api/v1/tokens", &serde_json::json!({ "name": name }))
    }

    pub fn revoke_api_token(&self, id: &str) -> Result<serde_json::Value> {
        self.delete(&format!("/api/v1/tokens/{id}"))
    }

    pub fn revoke_cli_login(&self) -> Result<serde_json::Value> {
        let refresh_token = self
            .auth
            .borrow()
            .stored_credentials
            .as_ref()
            .and_then(|creds| creds.refresh_token.clone())
            .ok_or_else(|| {
                anyhow::anyhow!("stored CLI credentials do not contain a refresh token")
            })?;

        let url = format!("{}/api/v1/auth/cli/revoke", self.base_url);
        let resp = self
            .client
            .post(&url)
            .json(&serde_json::json!({ "refresh_token": refresh_token }))
            .send()
            .with_context(|| format!("request to {} failed", url))?;

        self.handle_response(resp)
    }

    fn send_json_request(
        &self,
        method: Method,
        path: &str,
        body: Option<&serde_json::Value>,
    ) -> Result<serde_json::Value> {
        let access_token = self.auth.borrow().access_token.clone();
        let resp = self.send_authorized_request(method.clone(), path, body, &access_token)?;
        if resp.status() == StatusCode::UNAUTHORIZED && self.can_refresh() {
            self.refresh_cli_access_token()?;
            let access_token = self.auth.borrow().access_token.clone();
            let retry = self.send_authorized_request(method, path, body, &access_token)?;
            return self.handle_response(retry);
        }
        self.handle_response(resp)
    }

    fn send_authorized_request(
        &self,
        method: Method,
        path: &str,
        body: Option<&serde_json::Value>,
        access_token: &str,
    ) -> Result<reqwest::blocking::Response> {
        let url = format!("{}{}", self.base_url, path);
        let mut req = self.client.request(method, &url).bearer_auth(access_token);
        if let Some(body) = body {
            req = req.json(body);
        }
        req.send()
            .with_context(|| format!("request to {} failed", url))
    }

    fn can_refresh(&self) -> bool {
        self.auth
            .borrow()
            .stored_credentials
            .as_ref()
            .and_then(|creds| creds.refresh_token.as_ref())
            .is_some()
    }

    fn refresh_cli_access_token(&self) -> Result<()> {
        let refresh_token = self
            .auth
            .borrow()
            .stored_credentials
            .as_ref()
            .and_then(|creds| creds.refresh_token.clone())
            .ok_or_else(|| {
                anyhow::anyhow!("stored CLI credentials do not support automatic refresh")
            })?;

        let url = format!("{}/api/v1/auth/cli/refresh", self.base_url);
        let resp = self
            .client
            .post(&url)
            .json(&serde_json::json!({ "refresh_token": refresh_token }))
            .send()
            .with_context(|| format!("request to {} failed", url))?;

        let payload = self.handle_response(resp)?;
        let access_token = payload["access_token"]
            .as_str()
            .context("refresh response missing access_token field")?
            .to_string();

        let stored_credentials = {
            let mut auth = self.auth.borrow_mut();
            auth.access_token = access_token.clone();
            if let Some(creds) = auth.stored_credentials.as_mut() {
                creds.access_token = access_token;
            }
            auth.stored_credentials.clone()
        };

        if let Some(creds) = stored_credentials {
            CredentialStore::new()?.save(&creds)?;
        }
        Ok(())
    }

    fn handle_response(&self, resp: reqwest::blocking::Response) -> Result<serde_json::Value> {
        let status = resp.status();

        if status == StatusCode::NO_CONTENT {
            return Ok(serde_json::json!({"status": "ok"}));
        }

        let body = resp.text().context("failed to read response body")?;

        if status.is_client_error() || status.is_server_error() {
            let message = response_error_message(status, &body);
            let token_source = self.auth.borrow().token_source;

            if status == StatusCode::UNAUTHORIZED && token_source == TokenSource::EnvVar {
                bail!(
                    "{} (using {} from environment; \
                     unset it to use your stored CLI credentials from 'gestalt auth login')",
                    message,
                    ENV_API_KEY,
                );
            }
            if status == StatusCode::UNAUTHORIZED && token_source == TokenSource::StoredCredentials
            {
                bail!(
                    "{} (stored CLI credentials may be expired or revoked; run 'gestalt auth login' to refresh them)",
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

fn response_error_message(status: StatusCode, body: &str) -> String {
    serde_json::from_str::<serde_json::Value>(body)
        .ok()
        .and_then(|v| extract_error_message(&v))
        .unwrap_or_else(|| format!("HTTP {}: {}", status.as_u16(), body))
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
