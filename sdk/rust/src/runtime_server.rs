use std::sync::Arc;

use async_trait::async_trait;
use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::auth::AuthProvider;
use crate::datastore::DatastoreProvider;
use crate::error::Result;
use crate::generated::v1::plugin_runtime_server::PluginRuntime;
use crate::generated::v1::{
    ConfigurePluginRequest, ConfigurePluginResponse, HealthCheckResponse, PluginKind,
    PluginMetadata,
};
use crate::api::RuntimeMetadata;
use crate::{CURRENT_PROTOCOL_VERSION, Provider};

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

struct AuthRuntime<P> {
    provider: Arc<P>,
}

struct DatastoreRuntime<P> {
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
impl_runtime_hooks!(AuthRuntime, AuthProvider);
impl_runtime_hooks!(DatastoreRuntime, DatastoreProvider);

#[derive(Clone)]
pub struct RuntimeServer {
    kind: PluginKind,
    provider: Arc<dyn RuntimeHooks>,
}

impl RuntimeServer {
    pub fn for_provider<P>(provider: Arc<P>) -> Self
    where
        P: Provider,
    {
        Self {
            kind: PluginKind::Integration,
            provider: Arc::new(ProviderRuntime { provider }),
        }
    }

    pub fn for_auth<P>(provider: Arc<P>) -> Self
    where
        P: AuthProvider,
    {
        Self {
            kind: PluginKind::Auth,
            provider: Arc::new(AuthRuntime { provider }),
        }
    }

    pub fn for_datastore<P>(provider: Arc<P>) -> Self
    where
        P: DatastoreProvider,
    {
        Self {
            kind: PluginKind::Datastore,
            provider: Arc::new(DatastoreRuntime { provider }),
        }
    }
}

#[tonic::async_trait]
impl PluginRuntime for RuntimeServer {
    async fn get_plugin_metadata(
        &self,
        _request: GrpcRequest<()>,
    ) -> std::result::Result<GrpcResponse<PluginMetadata>, Status> {
        let metadata = self.provider.metadata().unwrap_or_default();
        Ok(GrpcResponse::new(PluginMetadata {
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

    async fn configure_plugin(
        &self,
        request: GrpcRequest<ConfigurePluginRequest>,
    ) -> std::result::Result<GrpcResponse<ConfigurePluginResponse>, Status> {
        let request = request.into_inner();
        if request.protocol_version != CURRENT_PROTOCOL_VERSION {
            return Err(Status::failed_precondition(format!(
                "host requested protocol version {}, plugin requires {}",
                request.protocol_version, CURRENT_PROTOCOL_VERSION
            )));
        }
        let config = crate::catalog::object_map(request.config);
        self.provider
            .configure(&request.name, config)
            .await
            .map_err(|error| Status::unknown(format!("configure plugin: {}", error.message())))?;
        Ok(GrpcResponse::new(ConfigurePluginResponse {
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
                message: error.message().to_owned(),
            })),
        }
    }
}
