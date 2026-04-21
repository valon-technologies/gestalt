use std::sync::Arc;

use tonic::codegen::async_trait;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::api::RuntimeMetadata;
use crate::auth::AuthenticationProvider;
use crate::cache::CacheProvider;
use crate::error::Result;
use crate::generated::v1::provider_lifecycle_server::ProviderLifecycle;
use crate::generated::v1::{
    ConfigureProviderRequest, ConfigureProviderResponse, HealthCheckResponse, ProviderIdentity,
    ProviderKind,
};
use crate::rpc_status::{require_protocol_version, rpc_error_message, rpc_status};
use crate::secrets::SecretsProvider;
use crate::{CURRENT_PROTOCOL_VERSION, Provider, S3Provider, WorkflowProvider};

#[async_trait]
trait RuntimeHooks: Send + Sync {
    async fn configure(
        &self,
        name: &str,
        config: serde_json::Map<String, serde_json::Value>,
    ) -> Result<()>;

    fn metadata(&self) -> Option<RuntimeMetadata>;

    fn warnings(&self) -> Vec<String>;

    async fn health_check(&self) -> Result<()>;
}

struct ProviderRuntime<P> {
    provider: Arc<P>,
}

struct AuthenticationRuntime<P> {
    provider: Arc<P>,
}

struct CacheRuntime<P> {
    provider: Arc<P>,
}

struct SecretsRuntime<P> {
    provider: Arc<P>,
}

struct S3Runtime<P> {
    provider: Arc<P>,
}

struct WorkflowRuntime<P> {
    provider: Arc<P>,
}

macro_rules! impl_runtime_hooks {
    ($wrapper:ident, $trait_bound:path) => {
        #[async_trait]
        impl<P> RuntimeHooks for $wrapper<P>
        where
            P: $trait_bound,
        {
            async fn configure(
                &self,
                name: &str,
                config: serde_json::Map<String, serde_json::Value>,
            ) -> Result<()> {
                self.provider.configure(name, config).await
            }

            fn metadata(&self) -> Option<RuntimeMetadata> {
                self.provider.metadata()
            }

            fn warnings(&self) -> Vec<String> {
                self.provider.warnings()
            }

            async fn health_check(&self) -> Result<()> {
                self.provider.health_check().await
            }
        }
    };
}

impl_runtime_hooks!(ProviderRuntime, Provider);
impl_runtime_hooks!(AuthenticationRuntime, AuthenticationProvider);
impl_runtime_hooks!(CacheRuntime, CacheProvider);
impl_runtime_hooks!(SecretsRuntime, SecretsProvider);
impl_runtime_hooks!(S3Runtime, S3Provider);
impl_runtime_hooks!(WorkflowRuntime, WorkflowProvider);

#[derive(Clone)]
pub struct RuntimeServer {
    kind: ProviderKind,
    provider: Arc<dyn RuntimeHooks>,
}

impl RuntimeServer {
    pub fn for_provider<P>(provider: Arc<P>) -> Self
    where
        P: Provider,
    {
        Self {
            kind: ProviderKind::Integration,
            provider: Arc::new(ProviderRuntime { provider }),
        }
    }

    pub fn for_authentication<P>(provider: Arc<P>) -> Self
    where
        P: AuthenticationProvider,
    {
        Self {
            kind: ProviderKind::Authentication,
            provider: Arc::new(AuthenticationRuntime { provider }),
        }
    }

    #[allow(dead_code)]
    pub fn for_auth<P>(provider: Arc<P>) -> Self
    where
        P: AuthenticationProvider,
    {
        Self::for_authentication(provider)
    }

    pub fn for_cache<P>(provider: Arc<P>) -> Self
    where
        P: CacheProvider,
    {
        Self {
            kind: ProviderKind::Cache,
            provider: Arc::new(CacheRuntime { provider }),
        }
    }

    pub fn for_secrets<P>(provider: Arc<P>) -> Self
    where
        P: SecretsProvider,
    {
        Self {
            kind: ProviderKind::Secrets,
            provider: Arc::new(SecretsRuntime { provider }),
        }
    }

    pub fn for_s3<P>(provider: Arc<P>) -> Self
    where
        P: S3Provider,
    {
        Self {
            kind: ProviderKind::S3,
            provider: Arc::new(S3Runtime { provider }),
        }
    }

    pub fn for_workflow<P>(provider: Arc<P>) -> Self
    where
        P: WorkflowProvider,
    {
        Self {
            kind: ProviderKind::Workflow,
            provider: Arc::new(WorkflowRuntime { provider }),
        }
    }
}

#[tonic::async_trait]
impl ProviderLifecycle for RuntimeServer {
    async fn get_provider_identity(
        &self,
        _request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcResponse<ProviderIdentity>, Status> {
        let metadata = self.provider.metadata().unwrap_or_default();
        Ok(GrpcResponse::new(ProviderIdentity {
            kind: self.kind as i32,
            name: metadata.name,
            display_name: metadata.display_name,
            description: metadata.description,
            version: metadata.version,
            warnings: self.provider.warnings(),
            min_protocol_version: CURRENT_PROTOCOL_VERSION,
            max_protocol_version: CURRENT_PROTOCOL_VERSION,
        }))
    }

    async fn configure_provider(
        &self,
        request: GrpcRequest<ConfigureProviderRequest>,
    ) -> std::result::Result<GrpcResponse<ConfigureProviderResponse>, Status> {
        let request = request.into_inner();
        require_protocol_version(request.protocol_version, CURRENT_PROTOCOL_VERSION)?;
        let config = crate::catalog::object_map(request.config);
        self.provider
            .configure(&request.name, config)
            .await
            .map_err(|error| rpc_status("configure provider", error))?;
        Ok(GrpcResponse::new(ConfigureProviderResponse {
            protocol_version: CURRENT_PROTOCOL_VERSION,
        }))
    }

    async fn health_check(
        &self,
        _request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcResponse<HealthCheckResponse>, Status> {
        match self.provider.health_check().await {
            Ok(()) => Ok(GrpcResponse::new(HealthCheckResponse {
                ready: true,
                message: String::new(),
            })),
            Err(error) => Ok(GrpcResponse::new(HealthCheckResponse {
                ready: false,
                message: rpc_error_message("health check", &error),
            })),
        }
    }
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use tonic::Code;
    use tonic::Request as GrpcRequest;

    use super::*;
    use crate::error::INTERNAL_ERROR_MESSAGE;

    #[derive(Default)]
    struct HiddenRuntimeProvider;

    #[tonic::async_trait]
    impl Provider for HiddenRuntimeProvider {
        async fn configure(
            &self,
            _name: &str,
            _config: serde_json::Map<String, serde_json::Value>,
        ) -> Result<()> {
            Err(std::io::Error::other("disk exploded").into())
        }

        async fn health_check(&self) -> Result<()> {
            Err(std::io::Error::other("health failed").into())
        }
    }

    #[tokio::test]
    async fn configure_provider_sanitizes_hidden_internal_errors() {
        let server = RuntimeServer::for_provider(Arc::new(HiddenRuntimeProvider));

        let error = server
            .configure_provider(GrpcRequest::new(ConfigureProviderRequest {
                name: "broken".to_owned(),
                config: None,
                protocol_version: CURRENT_PROTOCOL_VERSION,
            }))
            .await
            .expect_err("configure provider should fail");
        assert_eq!(error.code(), Code::Unknown);
        assert_eq!(error.message(), "configure provider: internal error");
    }

    #[tokio::test]
    async fn health_check_sanitizes_hidden_internal_errors() {
        let server = RuntimeServer::for_provider(Arc::new(HiddenRuntimeProvider));

        let response = server
            .health_check(GrpcRequest::new(()))
            .await
            .expect("health check response")
            .into_inner();
        assert!(!response.ready);
        assert_eq!(response.message, INTERNAL_ERROR_MESSAGE);
    }
}
