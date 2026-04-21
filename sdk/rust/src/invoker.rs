use std::time::Duration;

use hyper_util::rt::TokioIo;
use serde::Serialize;
use tokio::net::UnixStream;
use tonic::transport::{Channel, Endpoint, Uri};
use tower::service_fn;

use crate::OperationResult;
use crate::generated::v1::{
    self as pb, plugin_invoker_client::PluginInvokerClient as ProtoPluginInvokerClient,
};

pub const ENV_PLUGIN_INVOKER_SOCKET: &str = "GESTALT_PLUGIN_INVOKER_SOCKET";

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
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct InvokeOptions {
    pub connection: String,
    pub instance: String,
}

pub struct PluginInvoker {
    client: ProtoPluginInvokerClient<Channel>,
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

        let channel = Endpoint::try_from("http://[::]:50051")?
            .connect_with_connector(service_fn(move |_: Uri| {
                let path = socket_path.clone();
                async move { UnixStream::connect(path).await.map(TokioIo::new) }
            }))
            .await?;

        Ok(Self {
            client: ProtoPluginInvokerClient::new(channel),
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

            Some(pb::PluginInvocationGrant {
                plugin: plugin.to_owned(),
                operations,
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

fn json_to_struct(
    value: serde_json::Value,
) -> std::result::Result<prost_types::Struct, PluginInvokerError> {
    let serde_json::Value::Object(fields) = value else {
        if value.is_null() {
            return Ok(prost_types::Struct::default());
        }
        return Err(PluginInvokerError::Protocol(
            "plugin invoker: params must serialize to a JSON object".to_string(),
        ));
    };

    Ok(prost_types::Struct {
        fields: fields
            .into_iter()
            .map(|(key, value)| (key, json_value_to_prost(value)))
            .collect(),
    })
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
