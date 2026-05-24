use std::collections::BTreeMap;
use std::time::SystemTime;

use tonic::codegen::async_trait;

use crate::Result;

#[derive(Clone, Debug, Default, PartialEq, Eq)]
/// Credential record stored by the host.
pub struct ExternalCredential {
    pub id: String,
    pub subject_id: String,
    pub instance: String,
    pub access_token: String,
    pub refresh_token: String,
    pub scopes: String,
    pub expires_at: Option<SystemTime>,
    pub last_refreshed_at: Option<SystemTime>,
    pub refresh_error_count: i32,
    pub metadata_json: String,
    pub created_at: Option<SystemTime>,
    pub updated_at: Option<SystemTime>,
    pub connection_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
/// Selects a host-managed external credential.
pub struct ExternalCredentialLookup {
    pub subject_id: String,
    pub instance: String,
    pub connection_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct UpsertExternalCredentialRequest {
    pub credential: Option<ExternalCredential>,
    pub preserve_timestamps: bool,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct GetExternalCredentialRequest {
    pub lookup: Option<ExternalCredentialLookup>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ListExternalCredentialsRequest {
    pub subject_id: String,
    pub instance: String,
    pub connection_id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ListExternalCredentialsResponse {
    pub credentials: Vec<ExternalCredential>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct DeleteExternalCredentialRequest {
    pub id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ExternalCredentialTokenExchangeDriver {
    pub r#type: String,
    pub target_principal: String,
    pub scopes: Vec<String>,
    pub lifetime_seconds: i32,
    pub endpoint: String,
    pub params: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ExternalCredentialAuthConfig {
    pub r#type: String,
    pub token: String,
    pub token_prefix: String,
    pub grant_type: String,
    pub token_url: String,
    pub client_id: String,
    pub client_secret: String,
    pub client_auth: String,
    pub token_exchange: String,
    pub scopes: Vec<String>,
    pub scope_param: String,
    pub scope_separator: String,
    pub token_params: BTreeMap<String, String>,
    pub refresh_params: BTreeMap<String, String>,
    pub accept_header: String,
    pub access_token_path: String,
    pub token_exchange_drivers: Vec<ExternalCredentialTokenExchangeDriver>,
    pub refresh_token: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ValidateExternalCredentialConfigRequest {
    pub provider: String,
    pub connection: String,
    pub connection_id: String,
    pub mode: String,
    pub auth: Option<ExternalCredentialAuthConfig>,
    pub connection_params: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ResolveExternalCredentialRequest {
    pub provider: String,
    pub connection: String,
    pub connection_id: String,
    pub mode: String,
    pub credential_subject_id: String,
    pub actor_subject_id: String,
    pub instance: String,
    pub auth: Option<ExternalCredentialAuthConfig>,
    pub connection_params: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ResolveExternalCredentialResponse {
    pub token: String,
    pub expires_at: Option<SystemTime>,
    pub metadata_json: String,
    pub params: BTreeMap<String, String>,
    pub credential: Option<ExternalCredential>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ExternalCredentialTokenResponse {
    pub access_token: String,
    pub refresh_token: String,
    pub expires_in: i32,
    pub token_type: String,
    pub extra_json: String,
    pub refresh_source: String,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ExchangeExternalCredentialRequest {
    pub provider: String,
    pub connection: String,
    pub connection_id: String,
    pub credential_subject_id: String,
    pub actor_subject_id: String,
    pub instance: String,
    pub auth: Option<ExternalCredentialAuthConfig>,
    pub credential_json: String,
    pub connection_params: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ExchangeExternalCredentialResponse {
    pub token_response: Option<ExternalCredentialTokenResponse>,
}

#[async_trait]
/// Fakeable external-credential client contract.
pub trait ExternalCredentialClient: Send + Sync {
    async fn upsert_credential(
        &self,
        request: UpsertExternalCredentialRequest,
    ) -> Result<ExternalCredential>;

    async fn get_credential(
        &self,
        request: GetExternalCredentialRequest,
    ) -> Result<ExternalCredential>;

    async fn list_credentials(
        &self,
        request: ListExternalCredentialsRequest,
    ) -> Result<ListExternalCredentialsResponse>;

    async fn delete_credential(&self, request: DeleteExternalCredentialRequest) -> Result<()>;

    async fn validate_credential_config(
        &self,
        request: ValidateExternalCredentialConfigRequest,
    ) -> Result<()>;

    async fn resolve_credential(
        &self,
        request: ResolveExternalCredentialRequest,
    ) -> Result<ResolveExternalCredentialResponse>;

    async fn exchange_credential(
        &self,
        request: ExchangeExternalCredentialRequest,
    ) -> Result<ExchangeExternalCredentialResponse>;
}
