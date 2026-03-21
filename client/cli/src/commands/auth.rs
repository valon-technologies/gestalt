use std::io::Write;

use anyhow::{bail, Context, Result};

use crate::api::{self, ApiClient};
use crate::credentials::{CredentialStore, Credentials};
use crate::output::{self, Format};

pub fn login(url_override: Option<&str>) -> Result<()> {
    let base_url = api::resolve_url(url_override)?;

    let listener =
        std::net::TcpListener::bind("127.0.0.1:0").context("failed to bind callback listener")?;
    let port = listener.local_addr()?.port();

    let state = format!("{:x}", rand_u64());

    let login_url = format!("{}/api/v1/auth/login", base_url);
    let client = reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(10))
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

    let (stream, _) = listener.accept().context("failed to accept callback")?;
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
    callback_url.query_pairs_mut().append_pair("code", &code);

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

    let callback_body: serde_json::Value = callback_resp
        .json()
        .context("failed to parse callback response")?;

    let token = callback_body["token"]
        .as_str()
        .context("callback response missing 'token' field")?;

    let store = CredentialStore::new()?;
    store.save(&Credentials {
        api_url: base_url,
        session_token: token.to_string(),
    })?;

    let _ = send_browser_response(&stream, "Login successful! You can close this tab.");
    output::print_success("Logged in successfully.");
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
    store.delete()?;
    output::print_success("Logged out. Credentials removed.");
    Ok(())
}

pub fn status(url_override: Option<&str>, format: Format) -> Result<()> {
    let authenticated = ApiClient::from_env(url_override).is_ok();
    match format {
        Format::Json => output::print_json(&serde_json::json!({"authenticated": authenticated})),
        Format::Table => {
            if authenticated {
                eprintln!("Authenticated.");
            } else {
                eprintln!("Not authenticated. Run 'gestalt auth login' or set GESTALT_API_KEY.");
            }
        }
    }
    Ok(())
}

fn rand_u64() -> u64 {
    use std::collections::hash_map::RandomState;
    use std::hash::{BuildHasher, Hasher};
    RandomState::new().build_hasher().finish()
}
