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
    #[error("plugin invoker: request handle is not available")]
    MissingRequestHandle,
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
pub struct InvokeOptions {
    pub connection: String,
    pub instance: String,
}

pub struct PluginInvoker {
    client: ProtoPluginInvokerClient<Channel>,
    request_handle: String,
}

impl PluginInvoker {
    pub async fn connect(
        request_handle: impl AsRef<str>,
    ) -> std::result::Result<Self, PluginInvokerError> {
        let request_handle = request_handle.as_ref().trim().to_owned();
        if request_handle.is_empty() {
            return Err(PluginInvokerError::MissingRequestHandle);
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
            request_handle,
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
                request_handle: self.request_handle.clone(),
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
