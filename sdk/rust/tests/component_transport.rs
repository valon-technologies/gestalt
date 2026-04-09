#[allow(dead_code)]
mod helpers;

use std::collections::BTreeMap;
use std::path::Path;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use gestalt_plugin_sdk::proto::v1::auth_plugin_client::AuthPluginClient;
use gestalt_plugin_sdk::proto::v1::datastore_plugin_client::DatastorePluginClient;
use gestalt_plugin_sdk::proto::v1::plugin_runtime_client::PluginRuntimeClient;
use gestalt_plugin_sdk::proto::v1::{
    BeginLoginRequest, CompleteLoginRequest, ConfigurePluginRequest,
    DeleteOAuthRegistrationRequest, DeleteStoredIntegrationTokenRequest, FindOrCreateUserRequest,
    GetApiTokenByHashRequest, GetOAuthRegistrationRequest, GetStoredIntegrationTokenRequest,
    GetUserRequest, ListApiTokensRequest, ListStoredIntegrationTokensRequest, OAuthRegistration,
    PluginKind, RevokeAllApiTokensRequest, RevokeApiTokenRequest, StoredApiToken,
    StoredIntegrationToken, StoredUser, ValidateExternalTokenRequest,
};
use gestalt_plugin_sdk::{AuthProvider, DatastoreProvider, RuntimeMetadata};
use hyper_util::rt::tokio::TokioIo;
use prost_types::Timestamp;
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
    ) -> gestalt_plugin_sdk::Result<()> {
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
    ) -> gestalt_plugin_sdk::Result<gestalt_plugin_sdk::BeginLoginResponse> {
        Ok(gestalt_plugin_sdk::BeginLoginResponse {
            authorization_url: format!("https://example.com/login?state={}", req.host_state),
            plugin_state: b"provider-state".to_vec(),
        })
    }

    async fn complete_login(
        &self,
        req: CompleteLoginRequest,
    ) -> gestalt_plugin_sdk::Result<gestalt_plugin_sdk::AuthenticatedUser> {
        Ok(gestalt_plugin_sdk::AuthenticatedUser {
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
    ) -> gestalt_plugin_sdk::Result<Option<gestalt_plugin_sdk::AuthenticatedUser>> {
        if token == "external-token" {
            return Ok(Some(gestalt_plugin_sdk::AuthenticatedUser {
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
    let _socket_guard =
        helpers::EnvGuard::set(gestalt_plugin_sdk::ENV_PLUGIN_SOCKET, socket.as_os_str());

    let provider = Arc::new(TestAuthProvider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt_plugin_sdk::runtime::serve_auth_provider(serve_provider)
            .await
            .expect("serve auth provider");
    });

    helpers::wait_for_socket(&socket).await;

    let channel = connect_unix(&socket).await;
    let mut runtime = PluginRuntimeClient::new(channel.clone());
    let mut auth = AuthPluginClient::new(channel);

    let metadata = runtime
        .get_plugin_metadata(())
        .await
        .expect("get plugin metadata")
        .into_inner();
    assert_eq!(
        PluginKind::try_from(metadata.kind)
            .expect("valid plugin kind")
            .as_str_name(),
        "PLUGIN_KIND_AUTH"
    );
    assert_eq!(metadata.name, "auth-example");
    assert_eq!(metadata.warnings, vec!["set OIDC_BASE_URL"]);

    let configured = runtime
        .configure_plugin(ConfigurePluginRequest {
            name: "auth-runtime".to_string(),
            config: Some(helpers::struct_from_json(
                serde_json::json!({ "issuer": "https://issuer" }),
            )),
            protocol_version: gestalt_plugin_sdk::CURRENT_PROTOCOL_VERSION,
        })
        .await
        .expect("configure plugin")
        .into_inner();
    assert_eq!(
        configured.protocol_version,
        gestalt_plugin_sdk::CURRENT_PROTOCOL_VERSION
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
    assert_eq!(begin.plugin_state, b"provider-state");

    let completed = auth
        .complete_login(CompleteLoginRequest {
            query: BTreeMap::from([("email".to_string(), "complete@example.com".to_string())]),
            plugin_state: b"provider-state".to_vec(),
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

#[derive(Default)]
struct TestDatastoreProvider {
    configured_name: Mutex<String>,
    users: Mutex<BTreeMap<String, StoredUser>>,
    integration_tokens: Mutex<BTreeMap<String, StoredIntegrationToken>>,
    api_tokens: Mutex<BTreeMap<String, StoredApiToken>>,
    oauth_registrations: Mutex<BTreeMap<(String, String), OAuthRegistration>>,
}

fn timestamp(seconds: i64) -> Option<Timestamp> {
    Some(Timestamp { seconds, nanos: 0 })
}

#[async_trait]
impl DatastoreProvider for TestDatastoreProvider {
    async fn configure(
        &self,
        name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> gestalt_plugin_sdk::Result<()> {
        *self.configured_name.lock().expect("lock configured_name") = name.to_string();
        Ok(())
    }

    fn metadata(&self) -> Option<RuntimeMetadata> {
        Some(RuntimeMetadata {
            name: "datastore-example".to_string(),
            display_name: "Datastore Example".to_string(),
            description: "Test datastore provider".to_string(),
            version: "0.1.0".to_string(),
        })
    }

    async fn health_check(&self) -> gestalt_plugin_sdk::Result<()> {
        Ok(())
    }

    async fn migrate(&self) -> gestalt_plugin_sdk::Result<()> {
        Ok(())
    }

    async fn get_user(&self, id: &str) -> gestalt_plugin_sdk::Result<Option<StoredUser>> {
        Ok(self.users.lock().expect("lock users").get(id).cloned())
    }

    async fn find_or_create_user(&self, email: &str) -> gestalt_plugin_sdk::Result<StoredUser> {
        let mut users = self.users.lock().expect("lock users");
        if let Some(user) = users.values().find(|user| user.email == email) {
            return Ok(user.clone());
        }
        let user = StoredUser {
            id: format!("usr_{}", users.len() + 1),
            email: email.to_string(),
            display_name: "SDK User".to_string(),
            created_at: timestamp(100),
            updated_at: timestamp(200),
        };
        users.insert(user.id.clone(), user.clone());
        Ok(user)
    }

    async fn put_integration_token(
        &self,
        token: StoredIntegrationToken,
    ) -> gestalt_plugin_sdk::Result<()> {
        self.integration_tokens
            .lock()
            .expect("lock integration tokens")
            .insert(token.id.clone(), token);
        Ok(())
    }

    async fn get_integration_token(
        &self,
        user_id: &str,
        integration: &str,
        connection: &str,
        instance: &str,
    ) -> gestalt_plugin_sdk::Result<Option<StoredIntegrationToken>> {
        Ok(self
            .integration_tokens
            .lock()
            .expect("lock integration tokens")
            .values()
            .find(|token| {
                token.user_id == user_id
                    && token.integration == integration
                    && token.connection == connection
                    && token.instance == instance
            })
            .cloned())
    }

    async fn list_integration_tokens(
        &self,
        user_id: &str,
        integration: &str,
        connection: &str,
    ) -> gestalt_plugin_sdk::Result<Vec<StoredIntegrationToken>> {
        Ok(self
            .integration_tokens
            .lock()
            .expect("lock integration tokens")
            .values()
            .filter(|token| {
                token.user_id == user_id
                    && token.integration == integration
                    && token.connection == connection
            })
            .cloned()
            .collect())
    }

    async fn delete_integration_token(&self, id: &str) -> gestalt_plugin_sdk::Result<()> {
        self.integration_tokens
            .lock()
            .expect("lock integration tokens")
            .remove(id);
        Ok(())
    }

    async fn put_api_token(&self, token: StoredApiToken) -> gestalt_plugin_sdk::Result<()> {
        self.api_tokens
            .lock()
            .expect("lock api tokens")
            .insert(token.id.clone(), token);
        Ok(())
    }

    async fn get_api_token_by_hash(
        &self,
        hashed_token: &str,
    ) -> gestalt_plugin_sdk::Result<Option<StoredApiToken>> {
        Ok(self
            .api_tokens
            .lock()
            .expect("lock api tokens")
            .values()
            .find(|token| token.hashed_token == hashed_token)
            .cloned())
    }

    async fn list_api_tokens(
        &self,
        user_id: &str,
    ) -> gestalt_plugin_sdk::Result<Vec<StoredApiToken>> {
        Ok(self
            .api_tokens
            .lock()
            .expect("lock api tokens")
            .values()
            .filter(|token| token.user_id == user_id)
            .cloned()
            .collect())
    }

    async fn revoke_api_token(&self, user_id: &str, id: &str) -> gestalt_plugin_sdk::Result<()> {
        let mut tokens = self.api_tokens.lock().expect("lock api tokens");
        if tokens
            .get(id)
            .map(|token| token.user_id.as_str() == user_id)
            .unwrap_or(false)
        {
            tokens.remove(id);
        }
        Ok(())
    }

    async fn revoke_all_api_tokens(&self, user_id: &str) -> gestalt_plugin_sdk::Result<i64> {
        let mut tokens = self.api_tokens.lock().expect("lock api tokens");
        let initial = tokens.len();
        tokens.retain(|_, token| token.user_id != user_id);
        Ok((initial - tokens.len()) as i64)
    }

    async fn get_oauth_registration(
        &self,
        auth_server_url: &str,
        redirect_uri: &str,
    ) -> gestalt_plugin_sdk::Result<Option<OAuthRegistration>> {
        Ok(self
            .oauth_registrations
            .lock()
            .expect("lock oauth registrations")
            .get(&(auth_server_url.to_string(), redirect_uri.to_string()))
            .cloned())
    }

    async fn put_oauth_registration(
        &self,
        registration: OAuthRegistration,
    ) -> gestalt_plugin_sdk::Result<()> {
        self.oauth_registrations
            .lock()
            .expect("lock oauth registrations")
            .insert(
                (
                    registration.auth_server_url.clone(),
                    registration.redirect_uri.clone(),
                ),
                registration,
            );
        Ok(())
    }

    async fn delete_oauth_registration(
        &self,
        auth_server_url: &str,
        redirect_uri: &str,
    ) -> gestalt_plugin_sdk::Result<()> {
        self.oauth_registrations
            .lock()
            .expect("lock oauth registrations")
            .remove(&(auth_server_url.to_string(), redirect_uri.to_string()));
        Ok(())
    }
}

#[tokio::test]
async fn serves_datastore_provider_and_runtime_over_unix_socket() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-datastore.sock");
    let _socket_guard =
        helpers::EnvGuard::set(gestalt_plugin_sdk::ENV_PLUGIN_SOCKET, socket.as_os_str());

    let provider = Arc::new(TestDatastoreProvider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt_plugin_sdk::runtime::serve_datastore_provider(serve_provider)
            .await
            .expect("serve datastore provider");
    });

    helpers::wait_for_socket(&socket).await;

    let channel = connect_unix(&socket).await;
    let mut runtime = PluginRuntimeClient::new(channel.clone());
    let mut datastore = DatastorePluginClient::new(channel);

    let metadata = runtime
        .get_plugin_metadata(())
        .await
        .expect("get plugin metadata")
        .into_inner();
    assert_eq!(
        PluginKind::try_from(metadata.kind)
            .expect("valid plugin kind")
            .as_str_name(),
        "PLUGIN_KIND_DATASTORE"
    );
    assert_eq!(metadata.name, "datastore-example");

    let health = runtime
        .health_check(())
        .await
        .expect("health check")
        .into_inner();
    assert!(health.ready);

    runtime
        .configure_plugin(ConfigurePluginRequest {
            name: "datastore-runtime".to_string(),
            config: Some(helpers::struct_from_json(
                serde_json::json!({ "dsn": "sqlite://memory" }),
            )),
            protocol_version: gestalt_plugin_sdk::CURRENT_PROTOCOL_VERSION,
        })
        .await
        .expect("configure plugin");

    datastore.migrate(()).await.expect("migrate");

    let user = datastore
        .find_or_create_user(FindOrCreateUserRequest {
            email: "sdk@example.com".to_string(),
        })
        .await
        .expect("find or create user")
        .into_inner();
    assert_eq!(user.email, "sdk@example.com");

    let fetched_user = datastore
        .get_user(GetUserRequest {
            id: user.id.clone(),
        })
        .await
        .expect("get user")
        .into_inner();
    assert_eq!(fetched_user.id, user.id);

    let integration_token = StoredIntegrationToken {
        id: "tok_123".to_string(),
        user_id: user.id.clone(),
        integration: "github".to_string(),
        connection: "default".to_string(),
        instance: "primary".to_string(),
        access_token_sealed: b"access".to_vec(),
        refresh_token_sealed: b"refresh".to_vec(),
        scopes: "repo".to_string(),
        expires_at: timestamp(300),
        last_refreshed_at: timestamp(250),
        refresh_error_count: 0,
        connection_params: BTreeMap::from([("tenant".to_string(), "acme".to_string())]),
        created_at: timestamp(100),
        updated_at: timestamp(200),
    };
    datastore
        .put_stored_integration_token(integration_token.clone())
        .await
        .expect("put integration token");
    let fetched_token = datastore
        .get_stored_integration_token(GetStoredIntegrationTokenRequest {
            user_id: user.id.clone(),
            integration: "github".to_string(),
            connection: "default".to_string(),
            instance: "primary".to_string(),
        })
        .await
        .expect("get integration token")
        .into_inner();
    assert_eq!(fetched_token.id, integration_token.id);
    let listed_tokens = datastore
        .list_stored_integration_tokens(ListStoredIntegrationTokensRequest {
            user_id: user.id.clone(),
            integration: "github".to_string(),
            connection: "default".to_string(),
        })
        .await
        .expect("list integration tokens")
        .into_inner();
    assert_eq!(listed_tokens.tokens.len(), 1);
    datastore
        .delete_stored_integration_token(DeleteStoredIntegrationTokenRequest {
            id: integration_token.id.clone(),
        })
        .await
        .expect("delete integration token");

    let api_token = StoredApiToken {
        id: "api_123".to_string(),
        user_id: user.id.clone(),
        name: "CLI".to_string(),
        hashed_token: "hash_123".to_string(),
        scopes: "read write".to_string(),
        expires_at: timestamp(400),
        created_at: timestamp(100),
        updated_at: timestamp(200),
    };
    datastore
        .put_api_token(api_token.clone())
        .await
        .expect("put api token");
    let fetched_api_token = datastore
        .get_api_token_by_hash(GetApiTokenByHashRequest {
            hashed_token: "hash_123".to_string(),
        })
        .await
        .expect("get api token by hash")
        .into_inner();
    assert_eq!(fetched_api_token.id, api_token.id);
    let listed_api_tokens = datastore
        .list_api_tokens(ListApiTokensRequest {
            user_id: user.id.clone(),
        })
        .await
        .expect("list api tokens")
        .into_inner();
    assert_eq!(listed_api_tokens.tokens.len(), 1);
    datastore
        .revoke_api_token(RevokeApiTokenRequest {
            user_id: user.id.clone(),
            id: api_token.id.clone(),
        })
        .await
        .expect("revoke api token");
    datastore
        .put_api_token(api_token.clone())
        .await
        .expect("put api token again");
    let revoked = datastore
        .revoke_all_api_tokens(RevokeAllApiTokensRequest {
            user_id: user.id.clone(),
        })
        .await
        .expect("revoke all api tokens")
        .into_inner();
    assert_eq!(revoked.revoked, 1);

    let registration = OAuthRegistration {
        auth_server_url: "https://issuer".to_string(),
        redirect_uri: "https://host/callback".to_string(),
        client_id: "client_123".to_string(),
        client_secret_sealed: b"secret".to_vec(),
        expires_at: timestamp(500),
        authorization_endpoint: "https://issuer/authorize".to_string(),
        token_endpoint: "https://issuer/token".to_string(),
        scopes_supported: "openid profile".to_string(),
        discovered_at: timestamp(450),
    };
    datastore
        .put_o_auth_registration(registration.clone())
        .await
        .expect("put oauth registration");
    let fetched_registration = datastore
        .get_o_auth_registration(GetOAuthRegistrationRequest {
            auth_server_url: registration.auth_server_url.clone(),
            redirect_uri: registration.redirect_uri.clone(),
        })
        .await
        .expect("get oauth registration")
        .into_inner();
    assert_eq!(fetched_registration.client_id, "client_123");
    datastore
        .delete_o_auth_registration(DeleteOAuthRegistrationRequest {
            auth_server_url: registration.auth_server_url.clone(),
            redirect_uri: registration.redirect_uri.clone(),
        })
        .await
        .expect("delete oauth registration");

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
