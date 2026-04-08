use gestalt_plugin_sdk as gestalt;

struct AuthProvider;

#[gestalt::async_trait]
impl gestalt::AuthProvider for AuthProvider {
    async fn begin_login(
        &self,
        _req: gestalt::BeginLoginRequest,
    ) -> gestalt::Result<gestalt::BeginLoginResponse> {
        Ok(gestalt::BeginLoginResponse::default())
    }

    async fn complete_login(
        &self,
        _req: gestalt::CompleteLoginRequest,
    ) -> gestalt::Result<gestalt::AuthenticatedUser> {
        Ok(gestalt::AuthenticatedUser::default())
    }
}

struct DatastoreProvider;

#[gestalt::async_trait]
impl gestalt::DatastoreProvider for DatastoreProvider {
    async fn health_check(&self) -> gestalt::Result<()> {
        Ok(())
    }

    async fn migrate(&self) -> gestalt::Result<()> {
        Ok(())
    }

    async fn get_user(&self, _id: &str) -> gestalt::Result<Option<gestalt::StoredUser>> {
        Ok(None)
    }

    async fn find_or_create_user(&self, _email: &str) -> gestalt::Result<gestalt::StoredUser> {
        Ok(gestalt::StoredUser::default())
    }

    async fn put_integration_token(
        &self,
        _token: gestalt::StoredIntegrationToken,
    ) -> gestalt::Result<()> {
        Ok(())
    }

    async fn get_integration_token(
        &self,
        _user_id: &str,
        _integration: &str,
        _connection: &str,
        _instance: &str,
    ) -> gestalt::Result<Option<gestalt::StoredIntegrationToken>> {
        Ok(None)
    }

    async fn list_integration_tokens(
        &self,
        _user_id: &str,
        _integration: &str,
        _connection: &str,
    ) -> gestalt::Result<Vec<gestalt::StoredIntegrationToken>> {
        Ok(Vec::new())
    }

    async fn delete_integration_token(&self, _id: &str) -> gestalt::Result<()> {
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

fn new_auth() -> AuthProvider {
    AuthProvider
}

fn new_datastore() -> DatastoreProvider {
    DatastoreProvider
}

mod auth_exports {
    use super::*;

    gestalt::export_auth_provider!(constructor = new_auth);
}

mod datastore_exports {
    use super::*;

    gestalt::export_datastore_provider!(constructor = new_datastore);
}

#[test]
fn export_macros_generate_entrypoints() {
    let auth_entrypoint: fn(&str) -> gestalt::Result<()> = auth_exports::__gestalt_serve_auth;
    let datastore_entrypoint: fn(&str) -> gestalt::Result<()> =
        datastore_exports::__gestalt_serve_datastore;
    let _ = (auth_entrypoint, datastore_entrypoint);
}
