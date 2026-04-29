use std::time::Duration;

use hyper_util::rt::TokioIo;
use serde::Serialize;
use tokio::net::UnixStream;
use tonic::Request;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, Endpoint, Uri};
use tower::service_fn;

use crate::OperationResult;
use crate::generated::v1::{
    self as pb, plugin_invoker_client::PluginInvokerClient as ProtoPluginInvokerClient,
};

type PluginInvokerTransport = InterceptedService<Channel, RelayTokenInterceptor>;

pub const ENV_PLUGIN_INVOKER_SOCKET: &str = "GESTALT_PLUGIN_INVOKER_SOCKET";
pub const ENV_PLUGIN_INVOKER_SOCKET_TOKEN: &str = "GESTALT_PLUGIN_INVOKER_SOCKET_TOKEN";
const PLUGIN_INVOKER_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

#[derive(Debug, thiserror::Error)]
pub enum PluginInvokerError {
    #[error("plugin invoker: invocation token is not available")]
    MissingInvocationToken,
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    #[error("{0}")]
    Status(#[from] tonic::Status),
    #[error("{0}")]
    Env(String),
    #[error("{0}")]
    Json(#[from] serde_json::Error),
    #[error("{0}")]
    Protocol(String),
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct InvocationGrant {
    pub plugin: String,
    pub operations: Vec<String>,
    pub surfaces: Vec<String>,
    pub all_operations: bool,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct InvokeOptions {
    pub connection: String,
    pub instance: String,
    pub idempotency_key: String,
}

pub struct PluginInvoker {
    client: ProtoPluginInvokerClient<PluginInvokerTransport>,
    invocation_token: String,
}

impl PluginInvoker {
    pub async fn connect(
        invocation_token: impl AsRef<str>,
    ) -> std::result::Result<Self, PluginInvokerError> {
        let invocation_token = invocation_token.as_ref().trim().to_owned();
        if invocation_token.is_empty() {
            return Err(PluginInvokerError::MissingInvocationToken);
        }

        let socket_path = std::env::var(ENV_PLUGIN_INVOKER_SOCKET).map_err(|_| {
            PluginInvokerError::Env(format!("{ENV_PLUGIN_INVOKER_SOCKET} is not set"))
        })?;
        let relay_token = std::env::var(ENV_PLUGIN_INVOKER_SOCKET_TOKEN).unwrap_or_default();

        let channel = match parse_plugin_invoker_target(&socket_path)? {
            PluginInvokerTarget::Unix(path) => {
                Endpoint::try_from("http://[::]:50051")?
                    .connect_with_connector(service_fn(move |_: Uri| {
                        let path = path.clone();
                        async move { UnixStream::connect(path).await.map(TokioIo::new) }
                    }))
                    .await?
            }
            PluginInvokerTarget::Tcp(address) => {
                Endpoint::from_shared(format!("http://{address}"))?
                    .connect()
                    .await?
            }
            PluginInvokerTarget::Tls(address) => {
                Endpoint::from_shared(format!("https://{address}"))?
                    .connect()
                    .await?
            }
        };

        Ok(Self {
            client: ProtoPluginInvokerClient::with_interceptor(
                channel,
                relay_token_interceptor(relay_token.trim())?,
            ),
            invocation_token,
        })
    }

    pub async fn invoke<P>(
        &mut self,
        plugin: &str,
        operation: &str,
        params: P,
        options: Option<InvokeOptions>,
    ) -> std::result::Result<OperationResult, PluginInvokerError>
    where
        P: Serialize,
    {
        let response = self
            .client
            .invoke(pb::PluginInvokeRequest {
                plugin: plugin.to_string(),
                operation: operation.to_string(),
                params: Some(json_to_struct(serde_json::to_value(params)?)?),
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
            PluginInvokerError::Protocol(format!(
                "plugin invoker: invalid response status {}",
                response.status
            ))
        })?;

        Ok(OperationResult {
            status,
            body: response.body,
        })
    }

    pub async fn invoke_graphql<V>(
        &mut self,
        plugin: &str,
        document: &str,
        variables: Option<V>,
        options: Option<InvokeOptions>,
    ) -> std::result::Result<OperationResult, PluginInvokerError>
    where
        V: Serialize,
    {
        let document = document.trim();
        if document.is_empty() {
            return Err(PluginInvokerError::Protocol(
                "plugin invoker: graphql document is required".to_string(),
            ));
        }

        let response = self
            .client
            .invoke_graph_ql(pb::PluginInvokeGraphQlRequest {
                plugin: plugin.to_string(),
                document: document.to_string(),
                variables: variables
                    .map(serde_json::to_value)
                    .transpose()?
                    .map(|value| json_to_optional_struct(value, "variables"))
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
            PluginInvokerError::Protocol(format!(
                "plugin invoker: invalid response status {}",
                response.status
            ))
        })?;

        Ok(OperationResult {
            status,
            body: response.body,
        })
    }

    pub async fn exchange_invocation_token(
        &mut self,
        grants: &[InvocationGrant],
        ttl: Option<Duration>,
    ) -> std::result::Result<String, PluginInvokerError> {
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

enum PluginInvokerTarget {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_plugin_invoker_target(
    raw_target: &str,
) -> Result<PluginInvokerTarget, PluginInvokerError> {
    let target = raw_target.trim();
    if target.is_empty() {
        return Err(PluginInvokerError::Env(
            "plugin invoker: transport target is required".to_string(),
        ));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(PluginInvokerError::Env(format!(
                "plugin invoker: tcp target {raw_target:?} is missing host:port"
            )));
        }
        return Ok(PluginInvokerTarget::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(PluginInvokerError::Env(format!(
                "plugin invoker: tls target {raw_target:?} is missing host:port"
            )));
        }
        return Ok(PluginInvokerTarget::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(PluginInvokerError::Env(format!(
                "plugin invoker: unix target {raw_target:?} is missing a socket path"
            )));
        }
        return Ok(PluginInvokerTarget::Unix(path.to_string()));
    }
    if target.contains("://") {
        let scheme = target.split("://").next().unwrap_or_default();
        return Err(PluginInvokerError::Env(format!(
            "plugin invoker: unsupported target scheme {scheme:?}"
        )));
    }
    Ok(PluginInvokerTarget::Unix(target.to_string()))
}

fn encode_invocation_grants(grants: &[InvocationGrant]) -> Vec<pb::PluginInvocationGrant> {
    grants
        .iter()
        .filter_map(|grant| {
            let plugin = grant.plugin.trim();
            if plugin.is_empty() {
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

            Some(pb::PluginInvocationGrant {
                plugin: plugin.to_owned(),
                operations,
                surfaces,
                all_operations: grant.all_operations,
            })
        })
        .collect()
}

fn duration_to_ttl_seconds(ttl: Duration) -> std::result::Result<i64, PluginInvokerError> {
    if ttl.is_zero() {
        return Ok(0);
    }

    let ttl_seconds = ttl.as_secs().max(1);
    i64::try_from(ttl_seconds).map_err(|_| {
        PluginInvokerError::Protocol(
            "plugin invoker: exchange token ttl exceeds supported range".to_string(),
        )
    })
}

fn relay_token_interceptor(token: &str) -> Result<RelayTokenInterceptor, PluginInvokerError> {
    let header = if token.trim().is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(token.to_string()).map_err(|err| {
            PluginInvokerError::Env(format!(
                "invalid plugin invoker relay token metadata: {err}"
            ))
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

fn json_to_struct(
    value: serde_json::Value,
) -> std::result::Result<prost_types::Struct, PluginInvokerError> {
    Ok(json_to_optional_struct(value, "params")?.unwrap_or_default())
}

fn json_to_optional_struct(
    value: serde_json::Value,
    field_name: &str,
) -> std::result::Result<Option<prost_types::Struct>, PluginInvokerError> {
    let serde_json::Value::Object(fields) = value else {
        if value.is_null() {
            return Ok(None);
        }
        return Err(PluginInvokerError::Protocol(format!(
            "plugin invoker: {field_name} must serialize to a JSON object"
        )));
    };

    Ok(Some(prost_types::Struct {
        fields: fields
            .into_iter()
            .map(|(key, value)| (key, json_value_to_prost(value)))
            .collect(),
    }))
}

fn json_value_to_prost(value: serde_json::Value) -> prost_types::Value {
    use prost_types::value::Kind;

    let kind = match value {
        serde_json::Value::Null => Kind::NullValue(0),
        serde_json::Value::Bool(boolean) => Kind::BoolValue(boolean),
        serde_json::Value::Number(number) => Kind::NumberValue(number.as_f64().unwrap_or_default()),
        serde_json::Value::String(string) => Kind::StringValue(string),
        serde_json::Value::Array(items) => Kind::ListValue(prost_types::ListValue {
            values: items.into_iter().map(json_value_to_prost).collect(),
        }),
        serde_json::Value::Object(fields) => Kind::StructValue(prost_types::Struct {
            fields: fields
                .into_iter()
                .map(|(key, value)| (key, json_value_to_prost(value)))
                .collect(),
        }),
    };

    prost_types::Value { kind: Some(kind) }
}
