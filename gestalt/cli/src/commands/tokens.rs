use anyhow::{Context, Result};
use std::sync::Arc;

use crate::api::ApiClient;
use crate::output::{self, Format};

use gestalt_sdk::public::auth::BearerAuth;
use gestalt_sdk::public::generated::app_client::IdentityClient;
use gestalt_sdk::public::generated::identity::{
    GetGrantRequest, ListGrantsRequest, RevokeGrantRequest, TokenRequest,
};
use gestalt_sdk::public::grpc_transport::{SyncGrpcTransport, dial_public_grpc_endpoint};

const GRANT_TYPE_TOKEN_EXCHANGE: &str = "urn:ietf:params:oauth:grant-type:token-exchange";
const SUBJECT_TOKEN_TYPE_ACCESS_TOKEN: &str = "urn:ietf:params:oauth:token-type:access_token";
const DEFAULT_CLIENT_ID: &str = "gestalt-cli";

fn identity_client(api: &ApiClient) -> Result<IdentityClient<SyncGrpcTransport>> {
    let transport = SyncGrpcTransport::from_endpoint(
        dial_public_grpc_endpoint(api.base_url())?,
        Arc::new(BearerAuth::new(api.token())),
    )
    .with_timeout(std::time::Duration::from_secs(30));
    Ok(IdentityClient::new(transport))
}

pub fn create(
    client: &ApiClient,
    _name: Option<&str>,
    scopes: &str,
    expires_in: i64,
    format: Format,
) -> Result<()> {
    let identity = identity_client(client)?;
    let request = TokenRequest {
        grant_type: GRANT_TYPE_TOKEN_EXCHANGE.to_string(),
        subject_token: client.token().to_string(),
        subject_token_type: SUBJECT_TOKEN_TYPE_ACCESS_TOKEN.to_string(),
        scope: scopes.to_string(),
        client_id: DEFAULT_CLIENT_ID.to_string(),
        expires_in,
        ..Default::default()
    };
    let resp = identity
        .token_sync(request)
        .context("failed to create token")?;

    let grant_id = resp.grant_id.clone();
    let token = resp.access_token.clone();
    let scopes_list: Vec<String> = if resp.scope.is_empty() {
        vec![]
    } else {
        resp.scope.split_whitespace().map(String::from).collect()
    };

    let resp_json = serde_json::json!({
        "id": grant_id,
        "token": token,
        "scopes": scopes_list,
    });

    match format {
        Format::Json => output::print_json(&resp_json),
        Format::Table => {
            if !token.is_empty() {
                output::print_success("Token created. Save it now; it won't be shown again.");
                println!("{}", token);
            } else {
                output::print_json(&resp_json);
            }
        }
    }

    Ok(())
}

pub fn list(client: &ApiClient, format: Format) -> Result<()> {
    let identity = identity_client(client)?;
    let grants_resp = identity
        .list_grants_sync(ListGrantsRequest::default())
        .context("failed to list tokens")?;

    let mut items = Vec::new();
    for grant_id in &grants_resp.grant_ids {
        let detail = match identity.get_grant_sync(GetGrantRequest {
            grant_id: grant_id.clone(),
        }) {
            Ok(d) => d,
            Err(_) => continue,
        };
        let scopes: Vec<String> = detail.scopes.iter().map(|s| s.scope.clone()).collect();
        let created_at = if detail.created_at > 0 {
            unix_to_rfc3339(detail.created_at)
        } else {
            String::new()
        };
        let expires_at = if detail.expires_at > 0 {
            unix_to_rfc3339(detail.expires_at)
        } else {
            String::new()
        };
        items.push(serde_json::json!({
            "id": grant_id,
            "scopes": scopes,
            "createdAt": created_at,
            "expiresAt": expires_at,
        }));
    }

    let resp = serde_json::Value::Array(items);

    match format {
        Format::Json => output::print_json(&resp),
        Format::Table => {
            let items = resp.as_array().unwrap_or(&Vec::new()).clone();
            let rows: Vec<Vec<String>> = items
                .iter()
                .map(|item| {
                    vec![
                        item["id"].as_str().unwrap_or("-").to_string(),
                        token_scopes_display(item),
                        item["createdAt"].as_str().unwrap_or("-").to_string(),
                        item["expiresAt"].as_str().unwrap_or("never").to_string(),
                    ]
                })
                .collect();
            output::print_table(&["ID", "Scopes", "Created", "Expires"], &rows);
        }
    }

    Ok(())
}

pub fn revoke(client: &ApiClient, id: &str, format: Format) -> Result<()> {
    let identity = identity_client(client)?;
    identity
        .revoke_grant_sync(RevokeGrantRequest {
            grant_id: id.to_string(),
        })
        .context("failed to revoke token")?;

    match format {
        Format::Json => output::print_json(&serde_json::json!({"status": "revoked"})),
        Format::Table => output::print_success(&format!("Token {} revoked.", id)),
    }

    Ok(())
}

fn token_scopes_display(item: &serde_json::Value) -> String {
    if let Some(scopes) = item.get("scopes").and_then(|v| v.as_array()) {
        let parts: Vec<String> = scopes
            .iter()
            .filter_map(|scope| scope.as_str().map(str::to_string))
            .collect();
        if parts.is_empty() {
            return "-".to_string();
        }
        return parts.join(" ");
    }
    "-".to_string()
}

fn unix_to_rfc3339(seconds: i64) -> String {
    use time::{OffsetDateTime, format_description::well_known::Rfc3339};
    OffsetDateTime::from_unix_timestamp(seconds)
        .ok()
        .and_then(|dt| dt.format(&Rfc3339).ok())
        .unwrap_or_default()
}
