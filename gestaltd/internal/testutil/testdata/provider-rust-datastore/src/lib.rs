use std::collections::BTreeMap;
use std::sync::Mutex;
use std::time::{SystemTime, UNIX_EPOCH};

use gestalt_plugin_sdk as gestalt;
use prost_types::Timestamp;

pub struct Provider {
    users: Mutex<BTreeMap<String, gestalt::StoredUser>>,
    tokens: Mutex<BTreeMap<String, gestalt::StoredIntegrationToken>>,
}

impl Provider {
    fn new() -> Self {
        Self {
            users: Mutex::new(BTreeMap::new()),
            tokens: Mutex::new(BTreeMap::new()),
        }
    }
}

#[gestalt::async_trait]
impl gestalt::DatastoreProvider for Provider {
    fn metadata(&self) -> Option<gestalt::RuntimeMetadata> {
        Some(gestalt::RuntimeMetadata {
            name: "generated-datastore".to_string(),
            display_name: "Generated Datastore".to_string(),
            description: "Generated Rust datastore provider".to_string(),
            version: "0.0.1-alpha.1".to_string(),
        })
    }

    fn warnings(&self) -> Vec<String> {
        vec!["generated datastore warning".to_string()]
    }

    async fn health_check(&self) -> gestalt::Result<()> {
        Ok(())
    }

    async fn migrate(&self) -> gestalt::Result<()> {
        Ok(())
    }

    async fn get_user(&self, id: &str) -> gestalt::Result<Option<gestalt::StoredUser>> {
        Ok(self.users.lock().expect("lock users").get(id).cloned())
    }

    async fn find_or_create_user(&self, email: &str) -> gestalt::Result<gestalt::StoredUser> {
        let mut users = self.users.lock().expect("lock users");
        if let Some(user) = users.get(email) {
            return Ok(user.clone());
        }
        let now = now_timestamp();
        let user = gestalt::StoredUser {
            id: email.to_string(),
            email: email.to_string(),
            display_name: String::new(),
            created_at: now.clone(),
            updated_at: now,
        };
        users.insert(email.to_string(), user.clone());
        Ok(user)
    }

    async fn put_integration_token(
        &self,
        token: gestalt::StoredIntegrationToken,
    ) -> gestalt::Result<()> {
        self.tokens
            .lock()
            .expect("lock tokens")
            .insert(token.user_id.clone(), token);
        Ok(())
    }

    async fn get_integration_token(
        &self,
        user_id: &str,
        _integration: &str,
        _connection: &str,
        _instance: &str,
    ) -> gestalt::Result<Option<gestalt::StoredIntegrationToken>> {
        Ok(self
            .tokens
            .lock()
            .expect("lock tokens")
            .get(user_id)
            .cloned())
    }

    async fn list_integration_tokens(
        &self,
        user_id: &str,
        _integration: &str,
        _connection: &str,
    ) -> gestalt::Result<Vec<gestalt::StoredIntegrationToken>> {
        Ok(self
            .tokens
            .lock()
            .expect("lock tokens")
            .get(user_id)
            .cloned()
            .into_iter()
            .collect())
    }

    async fn delete_integration_token(&self, id: &str) -> gestalt::Result<()> {
        self.tokens.lock().expect("lock tokens").remove(id);
        Ok(())
    }

    async fn put_api_token(&self, _token: gestalt::StoredApiToken) -> gestalt::Result<()> {
        Ok(())
    }

    async fn get_api_token_by_hash(
        &self,
        _hashed_token: &str,
    ) -> gestalt::Result<Option<gestalt::StoredApiToken>> {
        Ok(None)
    }

    async fn list_api_tokens(
        &self,
        _user_id: &str,
    ) -> gestalt::Result<Vec<gestalt::StoredApiToken>> {
        Ok(Vec::new())
    }

    async fn revoke_api_token(&self, _user_id: &str, _id: &str) -> gestalt::Result<()> {
        Ok(())
    }

    async fn revoke_all_api_tokens(&self, _user_id: &str) -> gestalt::Result<i64> {
        Ok(0)
    }
}

fn now_timestamp() -> Option<Timestamp> {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("unix epoch");
    Some(Timestamp {
        seconds: now.as_secs() as i64,
        nanos: 0,
    })
}

fn new() -> Provider {
    Provider::new()
}

gestalt::export_datastore_provider!(constructor = new);
