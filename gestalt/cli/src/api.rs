use anyhow::{Context, Result, bail};
use reqwest::Method;
use reqwest::StatusCode;
use reqwest::blocking::Client;
use reqwest::header::{self, HeaderValue};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

use crate::config::ConfigStore;
use crate::credentials::CredentialStore;
use crate::http;

pub const DEFAULT_URL: &str = "http://localhost:8080";
pub const ENV_API_KEY: &str = "GESTALT_API_KEY";
pub const ENV_URL: &str = "GESTALT_URL";
pub const PROJECT_CONFIG_DIR: &str = ".gestalt";
pub const PROJECT_CONFIG_FILE: &str = ".gestalt/config.json";
pub const AUTH_INFO_PATH: &str = "/api/v1/auth/info";
pub const AUTH_LOGIN_PATH: &str = "/api/v1/auth/login";
pub const AUTH_LOGIN_CALLBACK_PATH: &str = "/api/v1/auth/login/callback";

#[derive(Debug, Clone, Deserialize)]
pub struct AuthInfo {
    pub login_supported: bool,
}

pub fn normalize_url(url: &str) -> String {
    let trimmed = url.trim().trim_end_matches('/');
    if trimmed.contains("://") {
        trimmed.to_string()
    } else {
        let use_http = url::Url::parse(&format!("http://{trimmed}"))
            .ok()
            .and_then(|parsed| {
                parsed.host().map(|host| match host {
                    url::Host::Domain(domain) => domain.eq_ignore_ascii_case("localhost"),
                    url::Host::Ipv4(ip) => ip.is_loopback(),
                    url::Host::Ipv6(ip) => ip.is_loopback(),
                })
            })
            .unwrap_or(false);
        let scheme = if use_http { "http" } else { "https" };
        format!("{scheme}://{trimmed}")
    }
}

pub fn resolve_url(url_override: Option<&str>) -> Result<String> {
    match find_configured_url(url_override)? {
        Some(url) => Ok(url),
        None => bail_no_url_configured(),
    }
}

pub fn resolve_url_with_fallback(
    url_override: Option<&str>,
    fallback_url: Option<&str>,
) -> Result<String> {
    match find_configured_url(url_override)? {
        Some(url) => Ok(url),
        None => match fallback_url {
            Some(url) => Ok(normalize_url(url)),
            None => bail_no_url_configured(),
        },
    }
}

pub fn fetch_auth_info(base_url: &str) -> Result<AuthInfo> {
    let auth_info_url = format!("{}{}", base_url.trim_end_matches('/'), AUTH_INFO_PATH);
    let client = Client::builder()
        .timeout(std::time::Duration::from_secs(5))
        .build()
        .context("failed to build HTTP client")?;
    let resp = client
        .get(&auth_info_url)
        .send()
        .with_context(|| format!("failed to reach {}", auth_info_url))?;
    let status = resp.status();
    let body = resp.text().context("failed to read response body")?;

    if !status.is_success() {
        bail!(
            "request to {} failed (HTTP {}): {}",
            auth_info_url,
            status.as_u16(),
            body.chars().take(200).collect::<String>()
        );
    }

    serde_json::from_str(&body)
        .with_context(|| format!("server at {} returned non-JSON auth info", base_url))
}

pub fn server_auth_disabled(base_url: &str) -> Result<bool> {
    Ok(!fetch_auth_info(base_url)?.login_supported)
}

fn find_configured_url(url_override: Option<&str>) -> Result<Option<String>> {
    Ok(find_configured_url_with_source(url_override)?.map(|(url, _)| url))
}

fn find_configured_url_with_source(url_override: Option<&str>) -> Result<Option<(String, String)>> {
    if let Some(url) = url_override {
        return Ok(Some((normalize_url(url), "--url flag".to_string())));
    }
    if let Ok(url) = std::env::var(ENV_URL)
        && !url.trim().is_empty()
    {
        return Ok(Some((
            normalize_url(&url),
            format!("{ENV_URL} environment variable"),
        )));
    }
    if let Some((url, path)) = find_project_config_value("url")? {
        return Ok(Some((
            normalize_url(&url),
            format!("project config ({})", path.display()),
        )));
    }

    let config_store = ConfigStore::new()?;
    match config_store.get("url")? {
        Some(url) => Ok(Some((
            normalize_url(&url),
            format!("global config ({})", config_store.path().display()),
        ))),
        None => Ok(None),
    }
}

/// Returns the configured server URL and a human-readable description of its source,
/// or `None` if no URL is configured. Never fails — config parse errors are ignored.
pub fn describe_server_config(url_override: Option<&str>) -> Option<(String, String)> {
    find_configured_url_with_source(url_override).ok().flatten()
}

fn bail_no_url_configured() -> Result<String> {
    let config_store = ConfigStore::new()?;
    bail!(
        "no URL configured: use --url, {}, {}, or the user-local config file at {}",
        ENV_URL,
        PROJECT_CONFIG_FILE,
        config_store.path().display()
    )
}

fn find_project_config_value(key: &str) -> Result<Option<(String, PathBuf)>> {
    let mut dir = std::env::current_dir().context("failed to determine current directory")?;
    loop {
        let candidate = dir.join(PROJECT_CONFIG_FILE);
        if let Some(value) = read_project_config_value(&candidate, key)? {
            return Ok(Some((value, candidate)));
        }
        if !dir.pop() {
            return Ok(None);
        }
    }
}

fn read_project_config_value(path: &Path, key: &str) -> Result<Option<String>> {
    if !path.exists() {
        return Ok(None);
    }
    if !path.is_file() {
        bail!(
            "project config path {} exists but is not a file",
            path.display()
        );
    }

    let contents = std::fs::read_to_string(path)
        .with_context(|| format!("failed to read project config {}", path.display()))?;
    let map: BTreeMap<String, String> = serde_json::from_str(&contents)
        .with_context(|| format!("failed to parse project config {}", path.display()))?;
    Ok(map.get(key).cloned())
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TokenSource {
    Direct,
    EnvVar,
    StoredCredentials,
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
        if let Some(key) = std::env::var(ENV_API_KEY).ok().filter(|v| !v.is_empty()) {
            let base_url = resolve_url(url_override)?;
            let mut client = Self::new(&base_url, &key)?;
            client.token_source = TokenSource::EnvVar;
            return Ok(client);
        }

        let store = CredentialStore::new()?;
        let creds = match store.load()? {
            Some(creds) => creds,
            None => {
                bail!(
                    "not authenticated: set {} or run 'gestalt auth login'",
                    ENV_API_KEY
                )
            }
        };

        let base_url = resolve_url_with_fallback(url_override, creds.api_url())?;
        let mut client = Self::new(&base_url, &creds.api_token)?;
        client.token_source = TokenSource::StoredCredentials;
        Ok(client)
    }

    pub fn new(base_url: &str, token: &str) -> Result<Self> {
        let mut default_headers = header::HeaderMap::new();
        default_headers.insert(
            header::ACCEPT,
            HeaderValue::from_static(http::APPLICATION_JSON),
        );

        let client = Client::builder()
            .timeout(std::time::Duration::from_secs(30))
            .default_headers(default_headers)
            .build()
            .context("failed to build HTTP client")?;

        Ok(Self {
            client,
            base_url: base_url.trim_end_matches('/').to_string(),
            token: token.to_string(),
            token_source: TokenSource::Direct,
        })
    }

    pub fn get(&self, path: &str) -> Result<serde_json::Value> {
        self.send(Method::GET, path)
    }

    pub fn post<T>(&self, path: &str, body: &T) -> Result<serde_json::Value>
    where
        T: Serialize + ?Sized,
    {
        self.send_json(Method::POST, path, body)
    }

    pub fn post_form<T>(&self, path: &str, body: &T) -> Result<String>
    where
        T: Serialize + ?Sized,
    {
        self.send_form(Method::POST, path, body)
    }

    pub fn delete(&self, path: &str) -> Result<serde_json::Value> {
        self.send(Method::DELETE, path)
    }

    pub fn create_api_token(&self, name: &str) -> Result<serde_json::Value> {
        self.post("/api/v1/tokens", &serde_json::json!({ "name": name }))
    }

    pub fn revoke_api_token(&self, id: &str) -> Result<serde_json::Value> {
        self.delete(&format!("/api/v1/tokens/{id}"))
    }

    fn send(&self, method: Method, path: &str) -> Result<serde_json::Value> {
        self.send_request(method, path, None::<&serde_json::Value>)
    }

    fn send_json<T>(&self, method: Method, path: &str, body: &T) -> Result<serde_json::Value>
    where
        T: Serialize + ?Sized,
    {
        self.send_request(method, path, Some(body))
    }

    fn send_form<T>(&self, method: Method, path: &str, body: &T) -> Result<String>
    where
        T: Serialize + ?Sized,
    {
        let url = format!("{}{}", self.base_url, path);
        let encoded = serde_urlencoded::to_string(body).context("failed to encode form body")?;
        let resp = self
            .client
            .request(method, &url)
            .bearer_auth(&self.token)
            .header(
                header::CONTENT_TYPE,
                HeaderValue::from_static(http::APPLICATION_X_WWW_FORM_URLENCODED),
            )
            .body(encoded)
            .send()
            .map_err(|e| self.wrap_send_error(e, &url))?;
        self.handle_text_response(resp)
    }

    fn send_request<T>(
        &self,
        method: Method,
        path: &str,
        body: Option<&T>,
    ) -> Result<serde_json::Value>
    where
        T: Serialize + ?Sized,
    {
        let url = format!("{}{}", self.base_url, path);
        let request = self.client.request(method, &url).bearer_auth(&self.token);
        let request = match body {
            Some(body) => request.json(body),
            None => request,
        };
        let resp = request.send().map_err(|e| self.wrap_send_error(e, &url))?;
        self.handle_response(resp)
    }

    fn wrap_send_error(&self, err: reqwest::Error, url: &str) -> anyhow::Error {
        if err.is_connect() || err.is_timeout() {
            anyhow::anyhow!(
                "could not reach server at {}\n  \
                 Run 'gestalt auth status' for diagnostics.",
                self.base_url
            )
        } else {
            anyhow::anyhow!(err).context(format!("request to {} failed", url))
        }
    }

    fn handle_response(&self, resp: reqwest::blocking::Response) -> Result<serde_json::Value> {
        let status = resp.status();

        if status == StatusCode::NO_CONTENT {
            return Ok(serde_json::json!({"status": "ok"}));
        }

        let body = resp.text().context("failed to read response body")?;
        self.bail_on_error_response(status, &body)?;

        if body.is_empty() {
            return Ok(serde_json::json!({}));
        }

        serde_json::from_str(&body).context("failed to parse response JSON")
    }

    fn handle_text_response(&self, resp: reqwest::blocking::Response) -> Result<String> {
        let status = resp.status();
        let body = resp.text().context("failed to read response body")?;
        self.bail_on_error_response(status, &body)?;
        Ok(body)
    }

    fn bail_on_error_response(&self, status: StatusCode, body: &str) -> Result<()> {
        if status.is_client_error() || status.is_server_error() {
            let message = serde_json::from_str::<serde_json::Value>(body)
                .ok()
                .and_then(|v| extract_error_message(&v))
                .unwrap_or_else(|| format!("HTTP {}: {}", status.as_u16(), body));

            if status == StatusCode::UNAUTHORIZED && self.token_source == TokenSource::EnvVar {
                bail!(
                    "{} (using {} from environment; \
                     unset it to use your stored CLI API token from 'gestalt auth login')",
                    message,
                    ENV_API_KEY,
                );
            }
            if status == StatusCode::UNAUTHORIZED
                && self.token_source == TokenSource::StoredCredentials
            {
                bail!(
                    "{} (stored CLI API token may be expired or revoked; run 'gestalt auth login' to mint a new one)",
                    message,
                );
            }

            bail!("{}", message);
        }
        Ok(())
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
