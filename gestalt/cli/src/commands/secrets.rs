use anyhow::{Context, Result, bail};
use std::io::{IsTerminal, Read};
use std::path::PathBuf;

use crate::api::ApiClient;
use crate::output::{self, Format};

const MANAGED_SECRETS_PATH: &str = "/admin/api/v1/managed-secrets";
const MANAGED_SECRET_MAX_BYTES: usize = 64 * 1024;

pub fn dispatch(client: &ApiClient, command: SecretsCommands, format: Format) -> Result<()> {
    match command {
        SecretsCommands::Create(args) => create_or_rotate(client, args, format),
        SecretsCommands::Rotate(args) => create_or_rotate(client, args, format),
        SecretsCommands::List { owner_app } => list(client, owner_app.as_deref(), format),
        SecretsCommands::Describe { name } => describe(client, &name, format),
        SecretsCommands::Preflight => preflight(client, format),
        SecretsCommands::Retire { name, reason } => retire(client, &name, &reason, format),
    }
}

fn create_or_rotate(
    client: &ApiClient,
    args: ManagedSecretWriteArgs,
    format: Format,
) -> Result<()> {
    let value = read_value(&args)?;
    let generate = args.generate;
    let mut body = serde_json::json!({
        "name": args.name,
        "ownerApp": args.owner_app.unwrap_or_default(),
        "scope": args.scope.unwrap_or_else(|| "app".to_string()),
        "description": args.description.unwrap_or_default(),
        "reason": args.reason,
        "source": "cli",
        "requestId": args.request_id.unwrap_or_default(),
    });
    if generate {
        body["generate"] = serde_json::json!(true);
    } else {
        body["value"] = serde_json::json!(value);
    }

    let response = client
        .post(MANAGED_SECRETS_PATH, &body)
        .context("failed to write managed secret")?;
    if format == Format::Json {
        output::print_json(&response);
        return Ok(());
    }

    if let Some(generated) = response.get("generatedValue").and_then(|v| v.as_str())
        && !generated.is_empty()
    {
        println!("Generated value (shown once):");
        println!("{generated}");
    }
    let secret = &response["secret"];
    let version = &response["version"];
    println!("Secret: {}", text(secret, "name"));
    if let Some(owner) = secret.get("ownerApp").and_then(|v| v.as_str())
        && !owner.is_empty()
    {
        println!("Owner app: {owner}");
    }
    println!("Version: {}", number(version, "version"));
    println!("Created at: {}", text(version, "createdAt"));
    if response
        .get("rolloutRequired")
        .and_then(|v| v.as_bool())
        .unwrap_or(false)
    {
        println!("Rollout required: yes");
        println!(
            "Gestalt resolves this secret during startup; roll out the runtime to load the new version."
        );
    } else {
        println!("Rollout required: no");
    }
    Ok(())
}

fn list(client: &ApiClient, owner_app: Option<&str>, format: Format) -> Result<()> {
    let response = client
        .get(MANAGED_SECRETS_PATH)
        .context("failed to list managed secrets")?;
    if format == Format::Json {
        output::print_json(&response);
        return Ok(());
    }
    let Some(rows) = response.as_array() else {
        bail!("managed secret list response was not an array");
    };
    let headers = ["Name", "Owner app", "Scope", "Version", "Updated"];
    let mut table = Vec::new();
    for row in rows {
        if let Some(owner) = owner_app
            && row.get("ownerApp").and_then(|v| v.as_str()) != Some(owner)
        {
            continue;
        }
        table.push(vec![
            text(row, "name"),
            text(row, "ownerApp"),
            text(row, "scope"),
            row.get("currentVersion")
                .and_then(|v| v.as_i64())
                .map(|v| v.to_string())
                .unwrap_or_else(|| "-".to_string()),
            text(row, "updatedAt"),
        ]);
    }
    output::print_table(&headers, &table);
    Ok(())
}

fn describe(client: &ApiClient, name: &str, format: Format) -> Result<()> {
    let encoded = crate::api::encode_path_segment(name);
    let response = client
        .get(&format!("{MANAGED_SECRETS_PATH}/{encoded}"))
        .with_context(|| format!("failed to describe managed secret {name}"))?;
    if format == Format::Json {
        output::print_json(&response);
        return Ok(());
    }
    println!("Name: {}", text(&response, "name"));
    println!("Owner app: {}", text(&response, "ownerApp"));
    println!("Scope: {}", text(&response, "scope"));
    if let Some(description) = nonempty(text(&response, "description")) {
        println!("Description: {description}");
    }
    println!(
        "Current version: {}",
        response
            .get("currentVersion")
            .and_then(|v| v.as_i64())
            .unwrap_or_default()
    );
    println!("Created at: {}", text(&response, "createdAt"));
    println!("Updated at: {}", text(&response, "updatedAt"));
    if let Some(retired) = response.get("retiredAt").and_then(|v| v.as_str()) {
        println!("Retired at: {retired}");
    }
    if let Some(audit) = response.get("audit").and_then(|v| v.as_array()) {
        println!("Audit:");
        for row in audit {
            println!(
                "  {} v{} by {}: {}",
                text(row, "createdAt"),
                row.get("version")
                    .and_then(|v| v.as_i64())
                    .unwrap_or_default(),
                text(row, "actor"),
                text(row, "action"),
            );
        }
    }
    Ok(())
}

fn preflight(client: &ApiClient, format: Format) -> Result<()> {
    // The CLI accepts references on stdin, matching the deploy preflight API.
    let mut raw = String::new();
    std::io::stdin()
        .read_to_string(&mut raw)
        .context("failed to read preflight references from stdin")?;
    let references: serde_json::Value =
        serde_json::from_str(&raw).context("failed to parse preflight references")?;
    let response = client
        .post(&format!("{MANAGED_SECRETS_PATH}/preflight"), &references)
        .context("failed to run managed secret preflight")?;
    if format == Format::Json {
        output::print_json(&response);
        return Ok(());
    }
    let ok = response
        .get("ok")
        .and_then(|v| v.as_bool())
        .unwrap_or(false);
    println!("Status: {}", if ok { "PASS" } else { "FAIL" });
    if let Some(missing) = response.get("missing").and_then(|v| v.as_array()) {
        for row in missing {
            println!("Missing: {}", text(row, "name"));
            if let Some(app) = nonempty(text(row, "app")) {
                println!("  App: {app}");
            }
            if let Some(field) = nonempty(text(row, "field")) {
                println!("  Field: {field}");
            }
        }
    }
    if let Some(unreferenced) = response.get("unreferenced").and_then(|v| v.as_array()) {
        for row in unreferenced {
            println!("Unreferenced: {}", text(row, "name"));
        }
    }
    if !ok {
        std::process::exit(1);
    }
    Ok(())
}

fn retire(client: &ApiClient, name: &str, reason: &str, format: Format) -> Result<()> {
    let encoded = crate::api::encode_path_segment(name);
    let response = client
        .post(
            &format!("{MANAGED_SECRETS_PATH}/{encoded}/retire"),
            &serde_json::json!({ "reason": reason }),
        )
        .with_context(|| format!("failed to retire managed secret {name}"))?;
    if format == Format::Json {
        output::print_json(&response);
    } else {
        println!("Retired {name}");
    }
    Ok(())
}

fn read_value(args: &ManagedSecretWriteArgs) -> Result<String> {
    if args.generate {
        if args.value_file.is_some() {
            bail!("--generate and --file are mutually exclusive");
        }
        return Ok(String::new());
    }
    let raw: Vec<u8> = match args.value_file.as_ref() {
        Some(path) => {
            std::fs::read(path).with_context(|| format!("failed to read {}", path.display()))?
        }
        None => {
            if std::io::stdin().is_terminal() {
                let value = prompt_hidden_value().context("failed to read secret value")?;
                return Ok(value);
            }
            let mut buffer = Vec::new();
            std::io::stdin()
                .read_to_end(&mut buffer)
                .context("failed to read secret value from stdin")?;
            buffer
        }
    };
    if raw.len() > MANAGED_SECRET_MAX_BYTES {
        bail!("secret value exceeds 64 KiB");
    }
    let value = String::from_utf8(raw).context("secret value must be valid UTF-8")?;
    if value.is_empty() {
        bail!("secret value is required");
    }
    Ok(value)
}

fn prompt_hidden_value() -> Result<String> {
    let first = rpassword::prompt_password("Secret value: ")?;
    let second = rpassword::prompt_password("Confirm secret value: ")?;
    if first != second {
        bail!("secret values do not match");
    }
    if first.is_empty() {
        bail!("secret value is required");
    }
    Ok(first)
}

fn text(value: &serde_json::Value, key: &str) -> String {
    value
        .get(key)
        .and_then(|v| v.as_str())
        .map(str::to_string)
        .unwrap_or_else(|| "-".to_string())
}

fn number(value: &serde_json::Value, key: &str) -> i64 {
    value.get(key).and_then(|v| v.as_i64()).unwrap_or_default()
}

fn nonempty(value: String) -> Option<String> {
    if value.trim().is_empty() {
        None
    } else {
        Some(value)
    }
}

#[derive(Debug, Clone, clap::Args)]
pub struct ManagedSecretWriteArgs {
    /// Logical secret name. This name is also bound into the ciphertext as additional authenticated data.
    pub name: String,

    /// App that owns this secret.
    #[arg(long = "owner-app")]
    pub owner_app: Option<String>,

    /// Secret scope: app or shared.
    #[arg(long)]
    pub scope: Option<String>,

    /// Human-readable description.
    #[arg(long)]
    pub description: Option<String>,

    /// Why this value is being written.
    #[arg(long)]
    pub reason: String,

    /// Stable request identifier for automation retries.
    #[arg(long = "request-id")]
    pub request_id: Option<String>,

    /// Read exact value bytes from a file instead of stdin or a prompt.
    #[arg(long = "file")]
    pub value_file: Option<PathBuf>,

    /// Generate a 48-byte base64url secret and print it once.
    #[arg(long, default_value_t = false)]
    pub generate: bool,
}

#[derive(Debug, Clone, clap::Subcommand)]
pub enum SecretsCommands {
    /// Create a new managed secret version. Fails if the name already exists.
    Create(ManagedSecretWriteArgs),
    /// Rotate an existing managed secret by creating a new version.
    Rotate(ManagedSecretWriteArgs),
    /// List managed secret metadata. Values are never returned.
    List {
        /// Filter by owning app.
        #[arg(long = "owner-app")]
        owner_app: Option<String>,
    },
    /// Show one secret's metadata and recent audit history.
    Describe {
        /// Logical secret name.
        name: String,
    },
    /// Check configured references against stored secrets. Reads JSON references from stdin.
    Preflight,
    /// Mark a secret retired while retaining history.
    Retire {
        /// Logical secret name.
        name: String,
        /// Why the secret is being retired.
        #[arg(long)]
        reason: String,
    },
}
