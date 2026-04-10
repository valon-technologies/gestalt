#[allow(dead_code)]
mod helpers;

use std::collections::BTreeMap;
use std::path::Path;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use gestalt::proto::v1::auth_provider_client::AuthProviderClient;
use gestalt::proto::v1::provider_lifecycle_client::ProviderLifecycleClient;
use gestalt::proto::v1::{
    BeginLoginRequest, CompleteLoginRequest, ConfigureProviderRequest, ProviderKind,
    ValidateExternalTokenRequest,
};
use gestalt::{AuthProvider, RuntimeMetadata};
use hyper_util::rt::tokio::TokioIo;
use tokio::net::UnixStream;
use tonic::Code;
use tonic::codegen::async_trait;
use tonic::transport::Endpoint;
use tower::service_fn;

struct TestAuthProvider {
    configured_name: Mutex<String>,
}

impl Default for TestAuthProvider {
    fn default() -> Self {
        Self {
            configured_name: Mutex::new(String::new()),
        }
    }
}

#[async_trait]
impl AuthProvider for TestAuthProvider {
    async fn configure(
        &self,
        name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> gestalt::Result<()> {
        *self.configured_name.lock().expect("lock configured_name") = name.to_string();
        Ok(())
    }

    fn metadata(&self) -> Option<RuntimeMetadata> {
        Some(RuntimeMetadata {
            name: "auth-example".to_string(),
            display_name: "Auth Example".to_string(),
            description: "Test auth provider".to_string(),
            version: "0.1.0".to_string(),
        })
    }

    fn warnings(&self) -> Vec<String> {
        vec!["set OIDC_BASE_URL".to_string()]
    }

    async fn begin_login(
        &self,
        req: BeginLoginRequest,
    ) -> gestalt::Result<gestalt::BeginLoginResponse> {
        Ok(gestalt::BeginLoginResponse {
            authorization_url: format!("https://example.com/login?state={}", req.host_state),
            provider_state: b"provider-state".to_vec(),
        })
    }

    async fn complete_login(
        &self,
        req: CompleteLoginRequest,
    ) -> gestalt::Result<gestalt::AuthenticatedUser> {
        Ok(gestalt::AuthenticatedUser {
            subject: "sub_123".to_string(),
            email: req
                .query
                .get("email")
                .cloned()
                .unwrap_or_else(|| "sdk@example.com".to_string()),
            email_verified: true,
            display_name: "SDK User".to_string(),
            avatar_url: String::new(),
            claims: BTreeMap::from([("source".to_string(), "complete_login".to_string())]),
        })
    }

    async fn validate_external_token(
        &self,
        token: &str,
    ) -> gestalt::Result<Option<gestalt::AuthenticatedUser>> {
        if token == "external-token" {
            return Ok(Some(gestalt::AuthenticatedUser {
                subject: "sub_external".to_string(),
                email: "external@example.com".to_string(),
                email_verified: true,
                display_name: "External User".to_string(),
                avatar_url: String::new(),
                claims: BTreeMap::new(),
            }));
        }
        Ok(None)
    }

    fn session_ttl(&self) -> Option<Duration> {
        Some(Duration::from_secs(7200))
    }
}

#[tokio::test]
async fn serves_auth_provider_and_runtime_over_unix_socket() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-auth.sock");
    let _socket_guard = helpers::EnvGuard::set(gestalt::ENV_PROVIDER_SOCKET, socket.as_os_str());

    let provider = Arc::new(TestAuthProvider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_auth_provider(serve_provider)
            .await
            .expect("serve auth provider");
    });

    helpers::wait_for_socket(&socket).await;

    let channel = connect_unix(&socket).await;
    let mut runtime = ProviderLifecycleClient::new(channel.clone());
    let mut auth = AuthProviderClient::new(channel);

    let metadata = runtime
        .get_provider_identity(())
        .await
        .expect("get provider identity")
        .into_inner();
    assert_eq!(
        ProviderKind::try_from(metadata.kind)
            .expect("valid provider kind")
            .as_str_name(),
        "PROVIDER_KIND_AUTH"
    );
    assert_eq!(metadata.name, "auth-example");
    assert_eq!(metadata.warnings, vec!["set OIDC_BASE_URL"]);

    let configured = runtime
        .configure_provider(ConfigureProviderRequest {
            name: "auth-runtime".to_string(),
            config: Some(helpers::struct_from_json(
                serde_json::json!({ "issuer": "https://issuer" }),
            )),
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION,
        })
        .await
        .expect("configure provider")
        .into_inner();
    assert_eq!(
        configured.protocol_version,
        gestalt::CURRENT_PROTOCOL_VERSION
    );

    let begin = auth
        .begin_login(BeginLoginRequest {
            callback_url: "https://host/callback".to_string(),
            host_state: "host-state".to_string(),
            scopes: vec!["openid".to_string()],
            options: BTreeMap::new(),
        })
        .await
        .expect("begin login")
        .into_inner();
    assert!(begin.authorization_url.contains("host-state"));
    assert_eq!(begin.provider_state, b"provider-state");

    let completed = auth
        .complete_login(CompleteLoginRequest {
            query: BTreeMap::from([("email".to_string(), "complete@example.com".to_string())]),
            provider_state: b"provider-state".to_vec(),
            callback_url: "https://host/callback".to_string(),
        })
        .await
        .expect("complete login")
        .into_inner();
    assert_eq!(completed.email, "complete@example.com");

    let validated = auth
        .validate_external_token(ValidateExternalTokenRequest {
            token: "external-token".to_string(),
        })
        .await
        .expect("validate external token")
        .into_inner();
    assert_eq!(validated.subject, "sub_external");

    let err = auth
        .validate_external_token(ValidateExternalTokenRequest {
            token: "missing-token".to_string(),
        })
        .await
        .expect_err("unknown token should return not found");
    assert_eq!(err.code(), Code::NotFound);

    let session_settings = auth
        .get_session_settings(())
        .await
        .expect("get session settings")
        .into_inner();
    assert_eq!(session_settings.session_ttl_seconds, 7200);

    serve_task.abort();
    let _ = serve_task.await;
}

async fn connect_unix(path: &Path) -> tonic::transport::Channel {
    Endpoint::try_from("http://[::]:50051")
        .expect("endpoint")
        .connect_with_connector(service_fn({
            let path = path.to_path_buf();
            move |_| {
                let path = path.clone();
                async move { UnixStream::connect(path).await.map(TokioIo::new) }
            }
        }))
        .await
        .expect("connect channel")
}
