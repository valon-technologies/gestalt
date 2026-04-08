use anyhow::{Context, Result, bail};
use reqwest::{StatusCode, header};

use crate::api::{self, AUTH_LOGIN_CALLBACK_PATH, AUTH_LOGIN_PATH, ApiClient};
use crate::credentials::{CredentialStore, Credentials};
use crate::http;
use crate::output::{self, Format};

const BROWSER_RESPONSE_PAGE: &str = r#"<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="light dark">
  <style>
    :root { color-scheme: light dark; --surface: #fff; --border: #ece4dd; --fg: #231810; --muted: #5c543c; --warm: radial-gradient(140% 90% at 50% 100%, #EACCB8 0%, #FDFCF9 50%, #F8F6F3 80%); }
    @media (prefers-color-scheme: dark) { :root { --surface: #20190f; --border: #31231b; --fg: #f3ede6; --muted: #dcd2c6; --warm: radial-gradient(140% 90% at 50% 100%, #3D2808 0%, #1A1410 50%, #161110 80%); } }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; padding: 24px; background: var(--warm); color: var(--fg); font: 16px/1.5 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    main { width: min(100%, 440px); padding: 28px; border: 1px solid var(--border); border-radius: 16px; background: var(--surface); box-shadow: 0 12px 40px rgba(35, 24, 16, 0.12); }
    small { display: block; font-size: 11px; letter-spacing: 0.14em; text-transform: uppercase; color: var(--muted); }
    h1 { margin: 16px 0 0; font: 700 clamp(2rem, 4vw, 2.35rem) / 1.05 Georgia, "Times New Roman", serif; letter-spacing: -0.03em; }
    p { margin: 12px 0 0; color: var(--muted); }
  </style>
</head>
<body>
  <main>
    <small>Gestalt</small>
    <h1 id="title"></h1>
    <p id="detail"></p>
  </main>
  <script>
    const data = __DATA__;
    document.title = `${data.title} - Gestalt`;
    document.getElementById('title').textContent = data.title;
    document.getElementById('detail').textContent = data.detail;
  </script>
</body>
</html>
"#;

pub fn login(url_override: Option<&str>) -> Result<()> {
    login_with_browser_opener(url_override, |url| {
        open::that(url).map(|_| ()).map_err(Into::into)
    })
}

pub fn login_with_browser_opener<F>(url_override: Option<&str>, open_browser: F) -> Result<()>
where
    F: FnOnce(&str) -> Result<()>,
{
    if api::env_api_key_is_set() {
        bail!(
            "{} is set in your environment and takes priority over stored CLI credentials. \
             Unset it before logging in, or use the API key directly.",
            api::ENV_API_KEY,
        );
    }

    let base_url = api::resolve_url(url_override)?;
    if api::server_auth_disabled(&base_url).unwrap_or(false) {
        bail!("authentication is disabled on this server");
    }

    let listener =
        std::net::TcpListener::bind("127.0.0.1:0").context("failed to bind callback listener")?;
    let port = listener.local_addr()?.port();
    let state = random_hex_string();

    let login_url = format!("{}{}", base_url, AUTH_LOGIN_PATH);
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
    if open_browser(url).is_err() {
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
        let _ = send_browser_response(
            &stream,
            "Login failed",
            "The callback state did not match. Check the terminal for details.",
        );
        bail!("OAuth state mismatch - possible CSRF attack");
    }

    let code = callback_params
        .query_pairs()
        .find(|(k, _)| k == "code")
        .map(|(_, v)| v.into_owned())
        .context("callback did not contain an authorization code")?;

    let mut callback_url = url::Url::parse(&format!("{}{}", base_url, AUTH_LOGIN_CALLBACK_PATH))
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
        let _ = send_browser_response(
            &stream,
            "Login failed",
            "The browser flow finished, but the CLI could not exchange the code. Check the terminal for details.",
        );
        bail!(
            "code exchange failed (HTTP {}): {}",
            callback_status.as_u16(),
            text.chars().take(200).collect::<String>()
        );
    }

    let login_result: serde_json::Value = callback_resp
        .json()
        .context("callback response missing token response")?;
    let api_token = login_result["token"]
        .as_str()
        .context("callback response missing token field")?;
    let api_token_id = login_result["id"]
        .as_str()
        .context("callback response missing id field")?;

    let store = CredentialStore::new()?;
    store.save(&Credentials {
        api_url: Some(base_url),
        api_token: api_token.to_string(),
        api_token_id: api_token_id.to_string(),
    })?;

    let _ = send_browser_response(&stream, "Login successful", "You can close this tab.");
    output::print_success("Logged in successfully. Stored CLI API token.");
    Ok(())
}

fn send_browser_response(
    stream: &std::net::TcpStream,
    title: &str,
    detail: &str,
) -> std::io::Result<()> {
    let html = build_browser_response_html(title, detail);
    http::write_response(
        stream,
        StatusCode::OK,
        http::TEXT_HTML,
        &html,
        &[(header::CACHE_CONTROL.as_str(), http::CACHE_CONTROL_NO_STORE)],
    )
}

fn build_browser_response_html(title: &str, detail: &str) -> String {
    let data = serde_json::json!({
        "detail": detail,
        "title": title,
    });
    let data = data.to_string().replace('<', "\\u003c");
    BROWSER_RESPONSE_PAGE.replace("__DATA__", &data)
}

pub fn logout() -> Result<()> {
    let store = CredentialStore::new()?;
    match store.load() {
        Ok(Some(creds)) => match creds.api_url() {
            Some(api_url) => {
                let client = ApiClient::new(api_url, &creds.api_token)?;
                if let Err(err) = client.revoke_api_token(&creds.api_token_id) {
                    output::print_warning(&format!(
                        "Failed to revoke stored CLI token {}: {}",
                        creds.api_token_id, err
                    ));
                }
            }
            None => output::print_warning(&format!(
                "Stored CLI token {} has no associated API URL; removing credentials locally without remote revoke.",
                creds.api_token_id
            )),
        },
        Ok(None) => {}
        Err(err) => output::print_warning(&format!(
            "Stored credentials could not be read; removing them locally without remote revoke: {}",
            err
        )),
    }
    store.delete()?;
    output::print_success("Logged out. Credentials removed.");
    Ok(())
}

pub fn status(url_override: Option<&str>, format: Format) -> Result<()> {
    let has_env_key = api::env_api_key_is_set();
    let (has_stored_credentials, stored_credentials_error) = match CredentialStore::new()?.load() {
        Ok(Some(_)) => (true, None),
        Ok(None) => (false, None),
        Err(err) => (false, Some(err.to_string())),
    };
    let server_auth_disabled = api::resolve_url(url_override)
        .ok()
        .and_then(|url| api::server_auth_disabled(&url).ok())
        .unwrap_or(false);
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
                "login_supported": !server_auth_disabled,
                "source": source,
                "env_var_set": has_env_key,
                "stored_credentials": has_stored_credentials,
            }));
        }
        Format::Table => {
            if server_auth_disabled {
                eprintln!("Authentication is disabled on this server.");
                if has_env_key {
                    output::print_warning(&format!(
                        "{} is set, but browser login is unavailable on this server.",
                        api::ENV_API_KEY,
                    ));
                } else if has_stored_credentials {
                    output::print_warning(
                        "Stored CLI credentials are present. Run 'gestalt auth logout' to clear them.",
                    );
                }
                if let Some(err) = &stored_credentials_error {
                    output::print_warning(&format!(
                        "Stored CLI credentials could not be read: {}",
                        err
                    ));
                }
            } else if has_env_key {
                eprintln!("Configured via {} environment variable.", api::ENV_API_KEY);
                if has_stored_credentials {
                    output::print_warning(&format!(
                        "Stored CLI credentials also exist but are being ignored. \
                         Unset {} to use it.",
                        api::ENV_API_KEY,
                    ));
                }
                if let Some(err) = &stored_credentials_error {
                    output::print_warning(&format!(
                        "Stored CLI credentials could not be read: {}",
                        err
                    ));
                }
            } else if has_stored_credentials {
                eprintln!("Stored CLI credentials are present.");
            } else if let Some(err) = &stored_credentials_error {
                output::print_warning(&format!(
                    "Stored CLI credentials could not be read: {}",
                    err
                ));
                eprintln!(
                    "Not configured. Run 'gestalt auth login' or set {}.",
                    api::ENV_API_KEY
                );
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
