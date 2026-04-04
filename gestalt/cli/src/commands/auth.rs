use std::io::Write;

use anyhow::{Context, Result, bail};

use crate::api::{self, ApiClient};
use crate::credentials::{CredentialStore, Credentials};
use crate::output::{self, Format};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum BrowserResponseTone {
    Success,
    Error,
}

pub fn login(url_override: Option<&str>) -> Result<()> {
    if api::env_api_key_is_set() {
        bail!(
            "{} is set in your environment and takes priority over stored CLI credentials. \
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
        let _ = send_browser_response(
            &stream,
            "Login failed",
            "The callback state did not match. Check the terminal for details.",
            BrowserResponseTone::Error,
        );
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
        let _ = send_browser_response(
            &stream,
            "Login failed",
            "The browser flow finished, but the CLI could not exchange the code. Check the terminal for details.",
            BrowserResponseTone::Error,
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
        api_url: base_url,
        api_token: api_token.to_string(),
        api_token_id: api_token_id.to_string(),
    })?;

    let _ = send_browser_response(
        &stream,
        "Login successful",
        "You can close this tab and return to the CLI.",
        BrowserResponseTone::Success,
    );
    output::print_success("Logged in successfully. Stored CLI API token.");
    Ok(())
}

fn send_browser_response(
    stream: &std::net::TcpStream,
    title: &str,
    detail: &str,
    tone: BrowserResponseTone,
) -> std::io::Result<()> {
    let html = build_browser_response_html(title, detail, tone);
    let response = format!(
        "HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: {}\r\nCache-Control: no-store\r\nConnection: close\r\n\r\n{}",
        html.len(),
        html
    );
    (&*stream).write_all(response.as_bytes())
}

fn build_browser_response_html(title: &str, detail: &str, tone: BrowserResponseTone) -> String {
    let escaped_title = escape_html(title);
    let escaped_detail = escape_html(detail);
    let (tone_class, chip_label, icon_label, note) = match tone {
        BrowserResponseTone::Success => (
            "success",
            "CLI login complete",
            "OK",
            "Return to the terminal to keep going.",
        ),
        BrowserResponseTone::Error => (
            "error",
            "CLI login error",
            "!",
            "Return to the terminal for the exact error details.",
        ),
    };

    format!(
        r#"<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="light dark">
  <title>{escaped_title} - Gestalt</title>
  <style>
    :root {{
      color-scheme: light dark;
      --background: 36 28% 99%;
      --surface: 36 33% 97%;
      --surface-raised: 33 22% 93%;
      --border: 30 18% 87%;
      --foreground: 24 30% 11%;
      --alpha-dark: 35, 24, 16;
      --shadow: 0 4px 12px rgba(35, 24, 16, 0.1);
      --page-gradient: radial-gradient(140% 90% at 50% 100%, #EACCB8 0%, #FDFCF9 50%, #F8F6F3 80%);
      --success-bg: rgba(74, 124, 74, 0.14);
      --success-border: rgba(74, 124, 74, 0.24);
      --success-text: #2F5A2F;
      --error-bg: rgba(199, 80, 80, 0.14);
      --error-border: rgba(199, 80, 80, 0.22);
      --error-text: #9B2C2C;
    }}

    @media (prefers-color-scheme: dark) {{
      :root {{
        --background: 24 18% 8%;
        --surface: 24 14% 11%;
        --surface-raised: 24 12% 15%;
        --border: 24 10% 20%;
        --foreground: 33 20% 90%;
        --alpha-dark: 232, 222, 212;
        --shadow: 0 18px 36px rgba(10, 8, 6, 0.35);
        --page-gradient: radial-gradient(140% 90% at 50% 100%, #3D2808 0%, #1A1410 50%, #161110 80%);
        --success-bg: rgba(184, 223, 184, 0.16);
        --success-border: rgba(184, 223, 184, 0.26);
        --success-text: #DCEFDC;
        --error-bg: rgba(199, 80, 80, 0.18);
        --error-border: rgba(199, 80, 80, 0.28);
        --error-text: #F9C7C7;
      }}
    }}

    * {{
      box-sizing: border-box;
    }}

    html, body {{
      height: 100%;
    }}

    body {{
      margin: 0;
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 24px;
      background: var(--page-gradient);
      color: rgba(var(--alpha-dark), 1);
      font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }}

    .panel {{
      width: min(100%, 440px);
      border: 1px solid hsl(var(--border));
      border-radius: 16px;
      padding: 28px;
      background: hsl(var(--surface));
      box-shadow: var(--shadow);
      backdrop-filter: blur(14px);
    }}

    .eyebrow {{
      margin: 0;
      font-size: 11px;
      font-weight: 600;
      letter-spacing: 0.16em;
      text-transform: uppercase;
      color: rgba(var(--alpha-dark), 0.5);
    }}

    .chip {{
      display: inline-flex;
      align-items: center;
      gap: 8px;
      margin-top: 14px;
      padding: 7px 10px;
      border-radius: 999px;
      border: 1px solid transparent;
      font-size: 12px;
      font-weight: 600;
      letter-spacing: 0.01em;
    }}

    .chip::before {{
      content: "";
      width: 8px;
      height: 8px;
      border-radius: 999px;
      background: currentColor;
      opacity: 0.9;
    }}

    .chip.success {{
      background: var(--success-bg);
      border-color: var(--success-border);
      color: var(--success-text);
    }}

    .chip.error {{
      background: var(--error-bg);
      border-color: var(--error-border);
      color: var(--error-text);
    }}

    .icon {{
      width: 52px;
      height: 52px;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      margin-top: 20px;
      border-radius: 14px;
      font-size: 22px;
      font-weight: 700;
      background: hsl(var(--surface-raised));
      border: 1px solid rgba(var(--alpha-dark), 0.08);
      color: rgba(var(--alpha-dark), 0.9);
      font-family: Georgia, "Times New Roman", serif;
    }}

    h1 {{
      margin: 20px 0 0;
      font-family: Georgia, "Times New Roman", serif;
      font-size: clamp(2rem, 4vw, 2.35rem);
      line-height: 1.05;
      letter-spacing: -0.03em;
    }}

    p {{
      margin: 14px 0 0;
      font-size: 15px;
      line-height: 1.65;
      color: rgba(var(--alpha-dark), 0.72);
    }}

    .helper {{
      margin-top: 18px;
      padding-top: 18px;
      border-top: 1px solid rgba(var(--alpha-dark), 0.08);
      font-size: 13px;
      color: rgba(var(--alpha-dark), 0.54);
    }}
  </style>
</head>
<body>
  <main class="panel">
    <p class="eyebrow">Gestalt CLI</p>
    <div class="chip {tone_class}">{chip_label}</div>
    <div class="icon" aria-hidden="true">{icon_label}</div>
    <h1>{escaped_title}</h1>
    <p>{escaped_detail}</p>
    <p class="helper">{note}</p>
  </main>
</body>
</html>
"#
    )
}

fn escape_html(value: &str) -> String {
    let mut escaped = String::with_capacity(value.len());
    for ch in value.chars() {
        match ch {
            '&' => escaped.push_str("&amp;"),
            '<' => escaped.push_str("&lt;"),
            '>' => escaped.push_str("&gt;"),
            '"' => escaped.push_str("&quot;"),
            '\'' => escaped.push_str("&#39;"),
            _ => escaped.push(ch),
        }
    }
    escaped
}

pub fn logout() -> Result<()> {
    let store = CredentialStore::new()?;
    match store.load() {
        Ok(Some(creds)) => {
            let client = ApiClient::new(&creds.api_url, &creds.api_token)?;
            if let Err(err) = client.revoke_api_token(&creds.api_token_id) {
                output::print_warning(&format!(
                    "Failed to revoke stored CLI token {}: {}",
                    creds.api_token_id, err
                ));
            }
        }
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

pub fn status(_url_override: Option<&str>, format: Format) -> Result<()> {
    let has_env_key = api::env_api_key_is_set();
    let (has_stored_credentials, stored_credentials_error) = match CredentialStore::new()?.load() {
        Ok(Some(_)) => (true, None),
        Ok(None) => (false, None),
        Err(err) => (false, Some(err.to_string())),
    };
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn browser_response_html_carries_gestalt_theme_tokens() {
        let html = build_browser_response_html(
            "Login successful",
            "You can close this tab and return to the CLI.",
            BrowserResponseTone::Success,
        );

        assert!(html.contains("Gestalt CLI"));
        assert!(html.contains("color-scheme: light dark"));
        assert!(html.contains("@media (prefers-color-scheme: dark)"));
        assert!(html.contains("#EACCB8"));
        assert!(html.contains("#3D2808"));
        assert!(html.contains("CLI login complete"));
        assert!(html.contains("Return to the terminal to keep going."));
    }

    #[test]
    fn browser_response_html_escapes_dynamic_text() {
        let html = build_browser_response_html(
            "<script>alert('x')</script>",
            "Use \"quotes\" & close.",
            BrowserResponseTone::Error,
        );

        assert!(html.contains("&lt;script&gt;alert(&#39;x&#39;)&lt;/script&gt;"));
        assert!(html.contains("Use &quot;quotes&quot; &amp; close."));
        assert!(!html.contains("<script>alert('x')</script>"));
        assert!(html.contains("CLI login error"));
    }
}
