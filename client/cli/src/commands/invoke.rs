use anyhow::{Context, Result};

use crate::api::ApiClient;
use crate::output::{self, Format};

pub fn invoke(
    url_override: Option<&str>,
    integration: &str,
    operation: &str,
    params: &[(String, String)],
    format: Format,
) -> Result<()> {
    let client = ApiClient::from_env(url_override)?;
    let path = format!("/api/v1/{}/{}", integration, operation);

    let resp = if params.is_empty() {
        client
            .get(&path)
            .with_context(|| format!("failed to invoke {}.{}", integration, operation))?
    } else {
        client
            .post_params(&path, params)
            .with_context(|| format!("failed to invoke {}.{}", integration, operation))?
    };

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => output::print_json_table(&resp),
    }

    Ok(())
}
