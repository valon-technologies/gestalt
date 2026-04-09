use std::collections::BTreeMap;
use std::time::Duration;

use gestalt_plugin_sdk as gestalt;

pub struct Provider;

#[gestalt::async_trait]
impl gestalt::AuthProvider for Provider {
    fn metadata(&self) -> Option<gestalt::RuntimeMetadata> {
        Some(gestalt::RuntimeMetadata {
            name: "generated-auth".to_string(),
            display_name: "Generated Auth".to_string(),
            description: "Generated Rust auth provider".to_string(),
            version: "0.0.1-alpha.1".to_string(),
        })
    }

    async fn begin_login(
        &self,
        _req: gestalt::BeginLoginRequest,
    ) -> gestalt::Result<gestalt::BeginLoginResponse> {
        Ok(gestalt::BeginLoginResponse {
            authorization_url: "https://auth.example.test/login?state=idp-state&prompt=consent"
                .to_string(),
            provider_state: Vec::new(),
        })
    }

    async fn complete_login(
        &self,
        req: gestalt::CompleteLoginRequest,
    ) -> gestalt::Result<gestalt::AuthenticatedUser> {
        if req.query.get("state").map(String::as_str) != Some("idp-state") {
            return Err(gestalt::Error::bad_request("unexpected state"));
        }
        if req.query.get("prompt").map(String::as_str) != Some("consent") {
            return Err(gestalt::Error::bad_request("unexpected prompt"));
        }
        Ok(gestalt::AuthenticatedUser {
            email: "generated-auth@example.com".to_string(),
            display_name: "Generated Auth User".to_string(),
            ..gestalt::AuthenticatedUser::default()
        })
    }

    async fn validate_external_token(
        &self,
        token: &str,
    ) -> gestalt::Result<Option<gestalt::AuthenticatedUser>> {
        if token.is_empty() {
            return Err(gestalt::Error::bad_request("token is required"));
        }
        let email = if token.matches('.').count() == 2 {
            "jwt@example.com".to_string()
        } else {
            format!("{token}@example.com")
        };
        Ok(Some(gestalt::AuthenticatedUser {
            email,
            display_name: "Validated User".to_string(),
            claims: BTreeMap::new(),
            ..gestalt::AuthenticatedUser::default()
        }))
    }

    fn session_ttl(&self) -> Option<Duration> {
        Some(Duration::from_secs(90 * 60))
    }
}

fn new() -> Provider {
    Provider
}

gestalt::export_auth_provider!(constructor = new);
