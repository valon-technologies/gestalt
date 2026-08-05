use anyhow::{Context, Result};
use serde_json::Value;

use crate::api::ApiClient;
use crate::cli::UsersCommands;
use crate::output::{self, Format};

pub fn dispatch(client: &ApiClient, command: UsersCommands, format: Format) -> Result<()> {
    match command {
        UsersCommands::Lookup { email } => lookup(client, &email, format),
    }
}

fn lookup(client: &ApiClient, email: &str, format: Format) -> Result<()> {
    let email = email.trim();
    if email.is_empty() {
        anyhow::bail!("email is required");
    }

    let path = format!(
        "/api/v1/users/lookup?email={}",
        crate::api::encode_path_segment(email)
    );
    let resp = client
        .get(&path)
        .context("failed to look up user by email")?;

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => print_lookup_table(&resp),
    }
    Ok(())
}

fn print_lookup_table(value: &Value) {
    output::print_json_table(value);
}

#[cfg(test)]
mod tests {
    use super::lookup;
    use crate::api::ApiClient;
    use crate::output::Format;

    #[test]
    fn lookup_rejects_blank_email() {
        let client = ApiClient::new("http://localhost:8080", "token").expect("client");
        let err = lookup(&client, "  ", Format::Json)
            .expect_err("blank email should fail before request");
        assert!(err.to_string().contains("email is required"));
    }
}
