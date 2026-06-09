use anyhow::{Context, Result};
use serde_json::{Value, json};

use crate::api::ApiClient;
use crate::cli::AuthorizationCommands;
use crate::output::{self, Format};

const CHECK_ACCESS_PATH: &str = "/api/v1/authorization/check-access";

pub fn dispatch(client: &ApiClient, command: AuthorizationCommands, format: Format) -> Result<()> {
    match command {
        AuthorizationCommands::CheckAccess(args) => {
            let body = json!({
                "subject": {
                    "type": args.subject_type,
                    "id": args.subject_id,
                },
                "action": {
                    "name": args.action,
                },
                "resource": {
                    "type": args.resource_type,
                    "id": args.resource_id,
                },
            });
            let resp = client
                .post(CHECK_ACCESS_PATH, &body)
                .context("failed to check authorization access")?;
            print_value(&resp, format);
            Ok(())
        }
    }
}

fn print_value(value: &Value, format: Format) {
    match format {
        Format::Json => output::print_json(value),
        Format::Table => output::print_json_table(value),
    }
}
