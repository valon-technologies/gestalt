use async_trait::async_trait;

use crate::api::RuntimeMetadata;
use crate::error::{Error, Result};
pub use crate::generated::v1::{
    OAuthRegistration, StoredApiToken, StoredIntegrationToken, StoredUser,
};

#[async_trait]
pub trait DatastoreProvider: Send + Sync + 'static {
    async fn configure(
        &self,
        _name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> Result<()> {
        Ok(())
    }

    fn metadata(&self) -> Option<RuntimeMetadata> {
        None
    }

    fn warnings(&self) -> Vec<String> {
        Vec::new()
    }

    async fn health_check(&self) -> Result<()>;

    async fn close(&self) -> Result<()> {
        Ok(())
    }

    async fn migrate(&self) -> Result<()>;

    async fn get_user(&self, id: &str) -> Result<Option<StoredUser>>;

    async fn find_or_create_user(&self, email: &str) -> Result<StoredUser>;

    async fn put_integration_token(&self, token: StoredIntegrationToken) -> Result<()>;

    async fn get_integration_token(
        &self,
        user_id: &str,
        integration: &str,
        connection: &str,
        instance: &str,
    ) -> Result<Option<StoredIntegrationToken>>;

    async fn list_integration_tokens(
        &self,
        user_id: &str,
        integration: &str,
        connection: &str,
    ) -> Result<Vec<StoredIntegrationToken>>;

    async fn delete_integration_token(&self, id: &str) -> Result<()>;

    async fn put_api_token(&self, token: StoredApiToken) -> Result<()>;

    async fn get_api_token_by_hash(&self, hashed_token: &str) -> Result<Option<StoredApiToken>>;

    async fn list_api_tokens(&self, user_id: &str) -> Result<Vec<StoredApiToken>>;

    async fn revoke_api_token(&self, user_id: &str, id: &str) -> Result<()>;

    async fn revoke_all_api_tokens(&self, user_id: &str) -> Result<i64>;

    async fn get_oauth_registration(
        &self,
        _auth_server_url: &str,
        _redirect_uri: &str,
    ) -> Result<Option<OAuthRegistration>> {
        Err(Error::unimplemented(
            "datastore provider does not support oauth registrations",
        ))
    }

    async fn put_oauth_registration(&self, _registration: OAuthRegistration) -> Result<()> {
        Err(Error::unimplemented(
            "datastore provider does not support oauth registrations",
        ))
    }

    async fn delete_oauth_registration(
        &self,
        _auth_server_url: &str,
        _redirect_uri: &str,
    ) -> Result<()> {
        Err(Error::unimplemented(
            "datastore provider does not support oauth registrations",
        ))
    }
}
