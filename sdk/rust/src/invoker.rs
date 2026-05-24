use std::time::Duration;

use hyper_util::rt::TokioIo;
use serde::Serialize;
use tokio::net::UnixStream;
use tonic::Request;
use tonic::codegen::async_trait;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tower::service_fn;

use crate::OperationResult;
use crate::generated::v1::{
    self as pb, app_invoker_client::AppInvokerClient as ProtoAppInvokerClient,
};
use crate::protocol;

type AppInvokerTransport = InterceptedService<Channel, RelayTokenInterceptor>;

/// Environment variable containing the plugin-invoker host-service target.
pub const ENV_PLUGIN_INVOKER_SOCKET: &str = "GESTALT_HOST_SERVICE_SOCKET";
/// Environment variable containing the optional plugin-invoker relay token.
pub const ENV_PLUGIN_INVOKER_SOCKET_TOKEN: &str = "GESTALT_HOST_SERVICE_TOKEN";
const PLUGIN_INVOKER_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

#[derive(Debug, thiserror::Error)]
/// Errors returned by [`AppInvoker`].
pub enum AppInvokerError {
    /// The invocation token was empty.
    #[error("app invoker: invocation token is not available")]
    MissingInvocationToken,
    /// The host-service transport could not be created.
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    /// The host-service RPC returned a gRPC status.
    #[error("{0}")]
    Status(#[from] tonic::Status),
    /// Required environment or target configuration was invalid.
    #[error("{0}")]
    Env(String),
    /// Invocation parameters or variables could not be serialized.
    #[error("{0}")]
    Json(#[from] serde_json::Error),
    /// The host returned a protocol value the SDK could not represent.
    #[error("{0}")]
    Protocol(String),
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Grant included when exchanging an invocation token for a child token.
pub struct InvocationGrant {
    /// App name that the child token may invoke.
    pub plugin: String,
    /// Specific operation ids allowed by the child token.
    pub operations: Vec<String>,
    /// Surface names allowed by the child token.
    pub surfaces: Vec<String>,
    /// Whether the child token may invoke every operation on the app.
    pub all_operations: bool,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Options that select the target connection for an app invocation.
pub struct InvokeOptions {
    /// Connected account id or name to invoke against.
    pub connection: String,
    /// Provider instance id or name to invoke against.
    pub instance: String,
    /// Idempotency key forwarded to the target operation.
    pub idempotency_key: String,
}

#[async_trait]
/// Fakeable client contract for app invoker calls.
pub trait AppInvokerClient: Send {
    async fn invoke(
        &mut self,
        plugin: String,
        operation: String,
        params: serde_json::Value,
        options: Option<InvokeOptions>,
    ) -> std::result::Result<OperationResult, AppInvokerError>;
    async fn invoke_graphql(
        &mut self,
        plugin: String,
        document: String,
        variables: Option<serde_json::Value>,
        options: Option<InvokeOptions>,
    ) -> std::result::Result<OperationResult, AppInvokerError>;
    async fn exchange_invocation_token(
        &mut self,
        grants: &[InvocationGrant],
        ttl: Option<Duration>,
    ) -> std::result::Result<String, AppInvokerError>;
}

/// Client for invoking sibling app operations through the host.
pub struct AppInvoker {
    client: ProtoAppInvokerClient<AppInvokerTransport>,
    invocation_token: String,
}

impl AppInvoker {
    /// Connects to the app invoker with an invocation token from the host.
    pub async fn connect(
        invocation_token: impl AsRef<str>,
    ) -> std::result::Result<Self, AppInvokerError> {
        let invocation_token = invocation_token.as_ref().trim().to_owned();
        if invocation_token.is_empty() {
            return Err(AppInvokerError::MissingInvocationToken);
        }

        let socket_path = std::env::var(ENV_PLUGIN_INVOKER_SOCKET)
            .map_err(|_| AppInvokerError::Env(format!("{ENV_PLUGIN_INVOKER_SOCKET} is not set")))?;
        let relay_token = std::env::var(ENV_PLUGIN_INVOKER_SOCKET_TOKEN).unwrap_or_default();

        let channel = match parse_app_invoker_target(&socket_path)? {
            AppInvokerTarget::Unix(path) => {
                Endpoint::try_from("http://[::]:50051")?
                    .connect_with_connector(service_fn(move |_: Uri| {
                        let path = path.clone();
                        async move { UnixStream::connect(path).await.map(TokioIo::new) }
                    }))
                    .await?
            }
            AppInvokerTarget::Tcp(address) => {
                Endpoint::from_shared(format!("http://{address}"))?
                    .connect()
                    .await?
            }
            AppInvokerTarget::Tls(address) => {
                Endpoint::from_shared(format!("https://{address}"))?
                    .tls_config(ClientTlsConfig::new().with_native_roots())?
                    .connect()
                    .await?
            }
        };

        Ok(Self {
            client: ProtoAppInvokerClient::with_interceptor(
                channel,
                relay_token_interceptor(relay_token.trim())?,
            ),
            invocation_token,
        })
    }

    /// Invokes one operation on another app.
    pub async fn invoke<P>(
        &mut self,
        plugin: &str,
        operation: &str,
        params: P,
        options: Option<InvokeOptions>,
    ) -> std::result::Result<OperationResult, AppInvokerError>
    where
        P: Serialize,
    {
        let response = self
            .client
            .invoke(pb::AppInvokeRequest {
                app: plugin.to_string(),
                operation: operation.to_string(),
                params: Some(serializable_to_struct(params, "params")?),
                connection: options
                    .as_ref()
                    .map(|opts| opts.connection.clone())
                    .unwrap_or_default(),
                instance: options
                    .as_ref()
                    .map(|opts| opts.instance.clone())
                    .unwrap_or_default(),
                invocation_token: self.invocation_token.clone(),
                idempotency_key: options
                    .as_ref()
                    .map(|opts| opts.idempotency_key.trim().to_string())
                    .unwrap_or_default(),
            })
            .await?
            .into_inner();

        let status = u16::try_from(response.status).map_err(|_| {
            AppInvokerError::Protocol(format!(
                "app invoker: invalid response status {}",
                response.status
            ))
        })?;

        Ok(OperationResult {
            status,
            body: response.body,
        })
    }

    /// Invokes another plugin's GraphQL surface.
    pub async fn invoke_graphql<V>(
        &mut self,
        plugin: &str,
        document: &str,
        variables: Option<V>,
        options: Option<InvokeOptions>,
    ) -> std::result::Result<OperationResult, AppInvokerError>
    where
        V: Serialize,
    {
        let document = document.trim();
        if document.is_empty() {
            return Err(AppInvokerError::Protocol(
                "app invoker: graphql document is required".to_string(),
            ));
        }

        let response = self
            .client
            .invoke_graph_ql(pb::AppInvokeGraphQlRequest {
                app: plugin.to_string(),
                document: document.to_string(),
                variables: variables
                    .map(|value| serializable_to_optional_struct(value, "variables"))
                    .transpose()?
                    .flatten(),
                connection: options
                    .as_ref()
                    .map(|opts| opts.connection.clone())
                    .unwrap_or_default(),
                instance: options
                    .as_ref()
                    .map(|opts| opts.instance.clone())
                    .unwrap_or_default(),
                invocation_token: self.invocation_token.clone(),
                idempotency_key: options
                    .as_ref()
                    .map(|opts| opts.idempotency_key.trim().to_string())
                    .unwrap_or_default(),
            })
            .await?
            .into_inner();

        let status = u16::try_from(response.status).map_err(|_| {
            AppInvokerError::Protocol(format!(
                "app invoker: invalid response status {}",
                response.status
            ))
        })?;

        Ok(OperationResult {
            status,
            body: response.body,
        })
    }

    /// Exchanges this invocation token for a narrower child token.
    pub async fn exchange_invocation_token(
        &mut self,
        grants: &[InvocationGrant],
        ttl: Option<Duration>,
    ) -> std::result::Result<String, AppInvokerError> {
        let ttl_seconds = ttl
            .map(duration_to_ttl_seconds)
            .transpose()?
            .unwrap_or_default();
        let response = self
            .client
            .exchange_invocation_token(pb::ExchangeInvocationTokenRequest {
                parent_invocation_token: self.invocation_token.clone(),
                grants: encode_invocation_grants(grants),
                ttl_seconds,
            })
            .await?
            .into_inner();

        Ok(response.invocation_token)
    }
}

#[async_trait]
impl AppInvokerClient for AppInvoker {
    async fn invoke(
        &mut self,
        plugin: String,
        operation: String,
        params: serde_json::Value,
        options: Option<InvokeOptions>,
    ) -> std::result::Result<OperationResult, AppInvokerError> {
        AppInvoker::invoke(self, &plugin, &operation, params, options).await
    }

    async fn invoke_graphql(
        &mut self,
        plugin: String,
        document: String,
        variables: Option<serde_json::Value>,
        options: Option<InvokeOptions>,
    ) -> std::result::Result<OperationResult, AppInvokerError> {
        AppInvoker::invoke_graphql(self, &plugin, &document, variables, options).await
    }

    async fn exchange_invocation_token(
        &mut self,
        grants: &[InvocationGrant],
        ttl: Option<Duration>,
    ) -> std::result::Result<String, AppInvokerError> {
        AppInvoker::exchange_invocation_token(self, grants, ttl).await
    }
}

enum AppInvokerTarget {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_app_invoker_target(raw_target: &str) -> Result<AppInvokerTarget, AppInvokerError> {
    let target = raw_target.trim();
    if target.is_empty() {
        return Err(AppInvokerError::Env(
            "app invoker: transport target is required".to_string(),
        ));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(AppInvokerError::Env(format!(
                "app invoker: tcp target {raw_target:?} is missing host:port"
            )));
        }
        return Ok(AppInvokerTarget::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(AppInvokerError::Env(format!(
                "app invoker: tls target {raw_target:?} is missing host:port"
            )));
        }
        return Ok(AppInvokerTarget::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(AppInvokerError::Env(format!(
                "app invoker: unix target {raw_target:?} is missing a socket path"
            )));
        }
        return Ok(AppInvokerTarget::Unix(path.to_string()));
    }
    if target.contains("://") {
        let scheme = target.split("://").next().unwrap_or_default();
        return Err(AppInvokerError::Env(format!(
            "app invoker: unsupported target scheme {scheme:?}"
        )));
    }
    Ok(AppInvokerTarget::Unix(target.to_string()))
}

fn encode_invocation_grants(grants: &[InvocationGrant]) -> Vec<pb::AppInvocationGrant> {
    grants
        .iter()
        .filter_map(|grant| {
            let app = grant.plugin.trim();
            if app.is_empty() {
                return None;
            }
            let operations = grant
                .operations
                .iter()
                .map(|operation| operation.trim())
                .filter(|operation| !operation.is_empty())
                .map(ToOwned::to_owned)
                .collect();
            let surfaces = grant
                .surfaces
                .iter()
                .map(|surface| surface.trim())
                .filter(|surface| !surface.is_empty())
                .map(|surface| surface.to_ascii_lowercase())
                .collect();

            Some(pb::AppInvocationGrant {
                app: app.to_owned(),
                operations,
                surfaces,
                all_operations: grant.all_operations,
            })
        })
        .collect()
}

fn duration_to_ttl_seconds(ttl: Duration) -> std::result::Result<i64, AppInvokerError> {
    if ttl.is_zero() {
        return Ok(0);
    }

    let ttl_seconds = ttl.as_secs().max(1);
    i64::try_from(ttl_seconds).map_err(|_| {
        AppInvokerError::Protocol(
            "app invoker: exchange token ttl exceeds supported range".to_string(),
        )
    })
}

fn relay_token_interceptor(token: &str) -> Result<RelayTokenInterceptor, AppInvokerError> {
    let header = if token.trim().is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(token.to_string()).map_err(|err| {
            AppInvokerError::Env(format!("invalid app invoker relay token metadata: {err}"))
        })?)
    };
    Ok(RelayTokenInterceptor { header })
}

#[derive(Clone)]
struct RelayTokenInterceptor {
    header: Option<MetadataValue<tonic::metadata::Ascii>>,
}

impl Interceptor for RelayTokenInterceptor {
    fn call(&mut self, mut request: Request<()>) -> Result<Request<()>, tonic::Status> {
        if let Some(header) = self.header.clone() {
            request
                .metadata_mut()
                .insert(PLUGIN_INVOKER_RELAY_TOKEN_HEADER, header);
        }
        Ok(request)
    }
}

fn serializable_to_struct<T: Serialize>(
    value: T,
    field_name: &str,
) -> std::result::Result<prost_types::Struct, AppInvokerError> {
    let value = protocol::json_value_from_serializable(value)?;
    Ok(json_to_optional_struct(value, field_name)?.unwrap_or_default())
}

fn json_to_optional_struct(
    value: serde_json::Value,
    field_name: &str,
) -> std::result::Result<Option<prost_types::Struct>, AppInvokerError> {
    if value.is_null() {
        return Ok(None);
    }
    let serde_json::Value::Object(_) = &value else {
        return Err(AppInvokerError::Protocol(format!(
            "app invoker: {field_name} must serialize to a JSON object"
        )));
    };

    protocol::struct_from_json(value)
        .map(Some)
        .map_err(|err| AppInvokerError::Protocol(err.to_string()))
}

fn serializable_to_optional_struct<T: Serialize>(
    value: T,
    field_name: &str,
) -> std::result::Result<Option<prost_types::Struct>, AppInvokerError> {
    let value = protocol::json_value_from_serializable(value)?;
    json_to_optional_struct(value, field_name)
}
