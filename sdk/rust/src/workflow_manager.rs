use hyper_util::rt::TokioIo;
use serde::Serialize;
use tokio::net::UnixStream;
use tonic::Request;
use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Channel, ClientTlsConfig, Endpoint, Uri};
use tower::service_fn;

use crate::generated::v1::{
    self as pb,
    workflow_manager_host_client::WorkflowManagerHostClient as ProtoWorkflowManagerHostClient,
};
use crate::workflow::workflow_struct;

type WorkflowManagerTransport = InterceptedService<Channel, RelayTokenInterceptor>;

/// Environment variable containing the workflow-manager host-service target.
#[doc(hidden)]
pub const ENV_WORKFLOW_MANAGER_SOCKET: &str = "GESTALT_WORKFLOW_MANAGER_SOCKET";
/// Environment variable containing the optional workflow-manager relay token.
#[doc(hidden)]
pub const ENV_WORKFLOW_MANAGER_SOCKET_TOKEN: &str = "GESTALT_WORKFLOW_MANAGER_SOCKET_TOKEN";
const WORKFLOW_MANAGER_RELAY_TOKEN_HEADER: &str = "x-gestalt-host-service-relay-token";

pub type WorkflowManagerApplyDefinition = pb::WorkflowManagerApplyDefinitionRequest;
pub type WorkflowManagerGetDefinition = pb::WorkflowManagerGetDefinitionRequest;
pub type WorkflowManagerListDefinitions = pb::WorkflowManagerListDefinitionsRequest;
pub type WorkflowManagerDeleteDefinition = pb::WorkflowManagerDeleteDefinitionRequest;
pub type WorkflowManagerSetDefinitionPaused = pb::WorkflowManagerSetDefinitionPausedRequest;
pub type WorkflowManagerSetActivationPaused = pb::WorkflowManagerSetActivationPausedRequest;
pub type WorkflowManagerStartRun = pb::WorkflowManagerStartRunRequest;
pub type WorkflowManagerSignalRun = pb::WorkflowManagerSignalRunRequest;
pub type WorkflowManagerSignalOrStartRun = pb::WorkflowManagerSignalOrStartRunRequest;
pub type WorkflowManagerCancelRun = pb::WorkflowManagerCancelRunRequest;
pub type WorkflowManagerDeliverEvent = pb::WorkflowManagerDeliverEventRequest;

#[derive(Debug, thiserror::Error)]
/// Errors returned by [`WorkflowManager`].
pub enum WorkflowManagerError {
    /// The invocation token was empty.
    #[error("workflow manager: invocation token is not available")]
    MissingInvocationToken,
    /// The host-service transport could not be created.
    #[error("{0}")]
    Transport(#[from] tonic::transport::Error),
    /// The host-service RPC returned a gRPC status.
    #[error("{0}")]
    Status(#[from] tonic::Status),
    /// Plain input could not be converted into the protocol request shape.
    #[error("{0}")]
    Input(#[from] crate::Error),
    /// Required environment or target configuration was invalid.
    #[error("{0}")]
    Env(String),
}

impl pb::WorkflowManagerStartRunRequest {
    /// Sets run input from any JSON-object-like serializable value.
    pub fn with_input<T: Serialize>(mut self, value: T) -> crate::Result<Self> {
        self.input = Some(workflow_struct(value)?);
        Ok(self)
    }
}

impl pb::WorkflowManagerSignalOrStartRunRequest {
    /// Sets run input from any JSON-object-like serializable value.
    pub fn with_input<T: Serialize>(mut self, value: T) -> crate::Result<Self> {
        self.input = Some(workflow_struct(value)?);
        Ok(self)
    }
}

/// Client for applying definitions, starting runs, and delivering workflow events.
pub struct WorkflowManager {
    client: ProtoWorkflowManagerHostClient<WorkflowManagerTransport>,
    invocation_token: String,
    idempotency_key: String,
}

impl WorkflowManager {
    /// Connects to the workflow manager with an invocation token from the host.
    pub async fn connect(
        invocation_token: impl AsRef<str>,
    ) -> std::result::Result<Self, WorkflowManagerError> {
        Self::connect_with_idempotency_key(invocation_token, "").await
    }

    /// Connects with a default idempotency key for idempotent requests.
    pub async fn connect_with_idempotency_key(
        invocation_token: impl AsRef<str>,
        idempotency_key: impl AsRef<str>,
    ) -> std::result::Result<Self, WorkflowManagerError> {
        let invocation_token = invocation_token.as_ref().trim().to_owned();
        if invocation_token.is_empty() {
            return Err(WorkflowManagerError::MissingInvocationToken);
        }

        let socket_path = std::env::var(ENV_WORKFLOW_MANAGER_SOCKET).map_err(|_| {
            WorkflowManagerError::Env(format!("{ENV_WORKFLOW_MANAGER_SOCKET} is not set"))
        })?;
        let relay_token = std::env::var(ENV_WORKFLOW_MANAGER_SOCKET_TOKEN).unwrap_or_default();
        let channel = match parse_workflow_manager_target(&socket_path)? {
            WorkflowManagerTarget::Unix(path) => {
                Endpoint::try_from("http://[::]:50051")?
                    .connect_with_connector(service_fn(move |_: Uri| {
                        let path = path.clone();
                        async move { UnixStream::connect(path).await.map(TokioIo::new) }
                    }))
                    .await?
            }
            WorkflowManagerTarget::Tcp(address) => {
                Endpoint::from_shared(format!("http://{address}"))?
                    .connect()
                    .await?
            }
            WorkflowManagerTarget::Tls(address) => {
                Endpoint::from_shared(format!("https://{address}"))?
                    .tls_config(ClientTlsConfig::new().with_native_roots())?
                    .connect()
                    .await?
            }
        };

        Ok(Self {
            client: ProtoWorkflowManagerHostClient::with_interceptor(
                channel,
                relay_token_interceptor(relay_token.trim())?,
            ),
            invocation_token,
            idempotency_key: idempotency_key.as_ref().trim().to_owned(),
        })
    }

    /// Applies a workflow definition.
    pub async fn apply_definition(
        &mut self,
        mut input: WorkflowManagerApplyDefinition,
    ) -> std::result::Result<pb::ManagedWorkflowDefinition, WorkflowManagerError> {
        self.fill_invocation_and_idempotency(
            &mut input.invocation_token,
            &mut input.idempotency_key,
        );
        Ok(self.client.apply_definition(input).await?.into_inner())
    }

    /// Fetches one workflow definition.
    pub async fn get_definition(
        &mut self,
        mut input: WorkflowManagerGetDefinition,
    ) -> std::result::Result<pb::ManagedWorkflowDefinition, WorkflowManagerError> {
        input.invocation_token = self.invocation_token.clone();
        Ok(self.client.get_definition(input).await?.into_inner())
    }

    /// Lists workflow definitions.
    pub async fn list_definitions(
        &mut self,
        mut input: WorkflowManagerListDefinitions,
    ) -> std::result::Result<pb::WorkflowManagerListDefinitionsResponse, WorkflowManagerError> {
        input.invocation_token = self.invocation_token.clone();
        Ok(self.client.list_definitions(input).await?.into_inner())
    }

    /// Deletes a workflow definition.
    pub async fn delete_definition(
        &mut self,
        mut input: WorkflowManagerDeleteDefinition,
    ) -> std::result::Result<(), WorkflowManagerError> {
        input.invocation_token = self.invocation_token.clone();
        self.client.delete_definition(input).await?;
        Ok(())
    }

    /// Pauses or resumes a workflow definition.
    pub async fn set_definition_paused(
        &mut self,
        mut input: WorkflowManagerSetDefinitionPaused,
    ) -> std::result::Result<pb::ManagedWorkflowDefinition, WorkflowManagerError> {
        input.invocation_token = self.invocation_token.clone();
        Ok(self.client.set_definition_paused(input).await?.into_inner())
    }

    /// Pauses or resumes one workflow activation.
    pub async fn set_activation_paused(
        &mut self,
        mut input: WorkflowManagerSetActivationPaused,
    ) -> std::result::Result<pb::ManagedWorkflowDefinition, WorkflowManagerError> {
        input.invocation_token = self.invocation_token.clone();
        Ok(self.client.set_activation_paused(input).await?.into_inner())
    }

    /// Starts a workflow run.
    pub async fn start_run(
        &mut self,
        mut input: WorkflowManagerStartRun,
    ) -> std::result::Result<pb::ManagedWorkflowRun, WorkflowManagerError> {
        self.fill_invocation_and_idempotency(
            &mut input.invocation_token,
            &mut input.idempotency_key,
        );
        Ok(self.client.start_run(input).await?.into_inner())
    }

    /// Signals an existing workflow run.
    pub async fn signal_run(
        &mut self,
        mut input: WorkflowManagerSignalRun,
    ) -> std::result::Result<pb::ManagedWorkflowRunSignal, WorkflowManagerError> {
        input.invocation_token = self.invocation_token.clone();
        Ok(self.client.signal_run(input).await?.into_inner())
    }

    /// Signals a run or starts it when no matching run exists.
    pub async fn signal_or_start_run(
        &mut self,
        mut input: WorkflowManagerSignalOrStartRun,
    ) -> std::result::Result<pb::ManagedWorkflowRunSignal, WorkflowManagerError> {
        self.fill_invocation_and_idempotency(
            &mut input.invocation_token,
            &mut input.idempotency_key,
        );
        Ok(self.client.signal_or_start_run(input).await?.into_inner())
    }

    /// Cancels a workflow run.
    pub async fn cancel_run(
        &mut self,
        mut input: WorkflowManagerCancelRun,
    ) -> std::result::Result<pb::ManagedWorkflowRun, WorkflowManagerError> {
        input.invocation_token = self.invocation_token.clone();
        Ok(self.client.cancel_run(input).await?.into_inner())
    }

    /// Delivers an event into the workflow manager.
    pub async fn deliver_event(
        &mut self,
        mut input: WorkflowManagerDeliverEvent,
    ) -> std::result::Result<pb::WorkflowManagerDeliverEventResponse, WorkflowManagerError> {
        self.fill_invocation_and_idempotency(
            &mut input.invocation_token,
            &mut input.idempotency_key,
        );
        Ok(self.client.deliver_event(input).await?.into_inner())
    }

    fn fill_invocation_and_idempotency(
        &self,
        invocation_token: &mut String,
        idempotency_key: &mut String,
    ) {
        *invocation_token = self.invocation_token.clone();
        if idempotency_key.trim().is_empty() {
            *idempotency_key = self.idempotency_key.clone();
        }
    }
}

#[derive(Clone)]
struct RelayTokenInterceptor {
    token: Option<MetadataValue<tonic::metadata::Ascii>>,
}

impl Interceptor for RelayTokenInterceptor {
    fn call(
        &mut self,
        mut request: Request<()>,
    ) -> std::result::Result<Request<()>, tonic::Status> {
        if let Some(token) = self.token.clone() {
            request
                .metadata_mut()
                .insert(WORKFLOW_MANAGER_RELAY_TOKEN_HEADER, token);
        }
        Ok(request)
    }
}

fn relay_token_interceptor(
    token: &str,
) -> std::result::Result<RelayTokenInterceptor, WorkflowManagerError> {
    let trimmed = token.trim();
    let token = if trimmed.is_empty() {
        None
    } else {
        Some(MetadataValue::try_from(trimmed).map_err(|err| {
            WorkflowManagerError::Env(format!(
                "workflow manager: invalid relay token metadata: {err}"
            ))
        })?)
    };
    Ok(RelayTokenInterceptor { token })
}

enum WorkflowManagerTarget {
    Unix(String),
    Tcp(String),
    Tls(String),
}

fn parse_workflow_manager_target(
    raw: &str,
) -> std::result::Result<WorkflowManagerTarget, WorkflowManagerError> {
    let target = raw.trim();
    if target.is_empty() {
        return Err(WorkflowManagerError::Env(
            "workflow manager: transport target is required".to_string(),
        ));
    }
    if let Some(address) = target.strip_prefix("tcp://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(WorkflowManagerError::Env(format!(
                "workflow manager: tcp target {raw:?} is missing host:port"
            )));
        }
        return Ok(WorkflowManagerTarget::Tcp(address.to_string()));
    }
    if let Some(address) = target.strip_prefix("tls://") {
        let address = address.trim();
        if address.is_empty() {
            return Err(WorkflowManagerError::Env(format!(
                "workflow manager: tls target {raw:?} is missing host:port"
            )));
        }
        return Ok(WorkflowManagerTarget::Tls(address.to_string()));
    }
    if let Some(path) = target.strip_prefix("unix://") {
        let path = path.trim();
        if path.is_empty() {
            return Err(WorkflowManagerError::Env(format!(
                "workflow manager: unix target {raw:?} is missing a socket path"
            )));
        }
        return Ok(WorkflowManagerTarget::Unix(path.to_string()));
    }
    if target.contains("://") {
        return Err(WorkflowManagerError::Env(format!(
            "workflow manager: unsupported target scheme in {raw:?}"
        )));
    }
    Ok(WorkflowManagerTarget::Unix(target.to_string()))
}
