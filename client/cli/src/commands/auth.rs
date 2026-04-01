use std::io::Write;

use anyhow::{Context, Result, bail};

use crate::api::{self, ApiClient};
use crate::credentials::{CredentialStore, Credentials};
use crate::output::{self, Format};

#[derive(Debug, serde::Deserialize)]
struct LoginCallbackResponse {
    api_token: String,
    api_token_id: String,
}

pub fn login(url_override: Option<&str>) -> Result<()> {
    if api::env_api_key_is_set() {
        bail!(
            "{} is set in your environment and takes priority over session tokens. \
             Unset it before logging in, or use the API key directly.",
            api::ENV_API_KEY,
        );
    }

    let base_url = api::resolve_url(url_override)?;

    let listener =
        std::net::TcpListener::bind("127.0.0.1:0").context("failed to bind callback listener")?;
    let port = listener.local_addr()?.port();

    let state = random_hex_string();

    let login_url = format!("{}/api/v1/auth/login", base_url);
    let client = reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(10))
        .cookie_store(true)
        .build()
        .context("failed to build HTTP client")?;
    let resp = client
        .post(&login_url)
        .json(&serde_json::json!({"state": state, "callback_port": port}))
        .send()
        .with_context(|| format!("failed to reach {}", login_url))?;

    let status = resp.status();
    if !status.is_success() {
        let text = resp.text().unwrap_or_default();
        bail!(
            "login request to {} failed (HTTP {}): {}",
            login_url,
            status.as_u16(),
            text.chars().take(200).collect::<String>()
        );
    }

    let body: serde_json::Value = resp
        .json()
        .with_context(|| format!("server at {} returned non-JSON response", base_url))?;

    let url = body["url"]
        .as_str()
        .context("login response missing 'url' field")?;

    eprintln!("Opening browser for authentication...");
    eprintln!("If the browser doesn't open, visit: {}", url);

    if open::that(url).is_err() {
        eprintln!("Could not open browser automatically.");
    }

    eprintln!(
        "Waiting for login callback on http://127.0.0.1:{}/ ...",
        port
    );

    let (tx, rx) = std::sync::mpsc::channel();
    std::thread::spawn(move || {
        let _ = tx.send(listener.accept());
    });

    let (stream, _) = rx
        .recv_timeout(std::time::Duration::from_secs(300))
        .map_err(|_| anyhow::anyhow!("timed out waiting for login callback after 5 minutes"))?
        .context("failed to accept callback")?;
    let mut reader = std::io::BufReader::new(&stream);
    let mut request_line = String::new();
    std::io::BufRead::read_line(&mut reader, &mut request_line)?;

    let callback_params = request_line
        .split_whitespace()
        .nth(1)
        .and_then(|path| url::Url::parse(&format!("http://localhost{}", path)).ok())
        .context("failed to parse callback request")?;

    let callback_state = callback_params
        .query_pairs()
        .find(|(k, _)| k == "state")
        .map(|(_, v)| v.into_owned());
    if callback_state.as_deref() != Some(&state) {
        let _ = send_browser_response(&stream, "Login failed: state mismatch.");
        bail!("OAuth state mismatch — possible CSRF attack");
    }

    let code = callback_params
        .query_pairs()
        .find(|(k, _)| k == "code")
        .map(|(_, v)| v.into_owned())
        .context("callback did not contain an authorization code")?;

    let mut callback_url = url::Url::parse(&format!("{}/api/v1/auth/login/callback", base_url))
        .context("failed to build callback URL")?;
    callback_url
        .query_pairs_mut()
        .append_pair("code", &code)
        .append_pair("state", &state)
        .append_pair("cli", "1");

    let callback_resp = client
        .get(callback_url.as_str())
        .send()
        .context("failed to exchange authorization code")?;

    let callback_status = callback_resp.status();
    if !callback_status.is_success() {
        let text = callback_resp.text().unwrap_or_default();
        let _ = send_browser_response(&stream, "Login failed. Check the terminal for details.");
        bail!(
            "code exchange failed (HTTP {}): {}",
            callback_status.as_u16(),
            text.chars().take(200).collect::<String>()
        );
    }

    let login_result: LoginCallbackResponse = callback_resp
        .json()
        .context("callback response missing CLI API token")?;

    let store = CredentialStore::new()?;
    store.save(&Credentials {
        api_url: base_url,
        api_token: login_result.api_token,
        api_token_id: login_result.api_token_id,
    })?;

    let _ = send_browser_response(&stream, "Login successful! You can close this tab.");
    output::print_success("Logged in successfully. Stored CLI API token.");
    Ok(())
}

fn send_browser_response(stream: &std::net::TcpStream, message: &str) -> std::io::Result<()> {
    let html = format!(
        "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\n\r\n<html><body><h1>{message}</h1></body></html>"
    );
    (&*stream).write_all(html.as_bytes())
}

pub fn logout() -> Result<()> {
    let store = CredentialStore::new()?;
    if let Some(creds) = store.load()? {
        let client = ApiClient::new(&creds.api_url, &creds.api_token)?;
        if let Err(err) = client.revoke_api_token(&creds.api_token_id) {
            output::print_warning(&format!(
                "Failed to revoke stored CLI token {}: {}",
                creds.api_token_id, err
            ));
        }
    }
    store.delete()?;
    output::print_success("Logged out. Credentials removed.");
    Ok(())
}

pub fn status(_url_override: Option<&str>, format: Format) -> Result<()> {
    let has_env_key = api::env_api_key_is_set();
    let has_stored_credentials = CredentialStore::new()?.load()?.is_some();
    let configured = has_env_key || has_stored_credentials;

    match format {
        Format::Json => {
            let source = if has_env_key {
                "env"
            } else if has_stored_credentials {
                "api_token"
            } else {
                "none"
            };
            output::print_json(&serde_json::json!({
                "authenticated": configured,
                "source": source,
                "env_var_set": has_env_key,
                "stored_credentials": has_stored_credentials,
            }));
        }
        Format::Table => {
            if has_env_key {
                eprintln!("Configured via {} environment variable.", api::ENV_API_KEY);
                if has_stored_credentials {
                    output::print_warning(&format!(
                        "Stored CLI API token also exists but is being ignored. \
                         Unset {} to use it.",
                        api::ENV_API_KEY,
                    ));
                }
            } else if has_stored_credentials {
                eprintln!("Configured via stored CLI API token.");
            } else {
                eprintln!(
                    "Not configured. Run 'gestalt auth login' or set {}.",
                    api::ENV_API_KEY,
                );
            }
        }
    }
    Ok(())
}

fn random_hex_string() -> String {
    let mut buf = [0u8; 16];
    getrandom::fill(&mut buf).expect("failed to generate random bytes");
    buf.iter().map(|b| format!("{b:02x}")).collect()
}
