#![warn(rustdoc::broken_intra_doc_links)]
#![doc = include_str!("../README.md")]

mod agent;
mod agent_access;
mod api;
mod app_access;
mod app_decode;
mod auth;
mod auth_server;
mod cache;
mod cache_server;
mod catalog;
mod env;
mod error;
mod generated;
/// IndexedDB-style datastore client and provider helpers.
pub mod indexeddb;
mod protocol;
mod provider_server;
mod router;
mod rpc_status;
/// Runtime entrypoints for serving Gestalt provider surfaces over Unix sockets.
pub mod runtime;
mod runtime_log_host;
mod runtime_provider;
mod runtime_server;
/// S3-compatible client and provider helpers.
pub mod s3;
mod secrets;
mod secrets_server;
/// OpenTelemetry helpers for provider-authored GenAI instrumentation.
pub mod telemetry;
pub mod workflow;
mod workflow_access;

#[doc(hidden)]
pub mod proto {
    pub use crate::generated::v1;
}

pub use agent::{
    AgentCatalogToolConfig, AgentExecutionStatus, AgentHost, AgentHostApi, AgentHostError,
    AgentHostExecuteToolInput, AgentHostListToolsInput, AgentHostResolveConnectionInput,
    AgentInteraction, AgentInteractionState, AgentInteractionType, AgentJson, AgentMessage,
    AgentMessagePart, AgentMessagePartImageRef, AgentMessagePartToolCall,
    AgentMessagePartToolResult, AgentMessagePartType, AgentOutput, AgentPreparedWorkspace,
    AgentProvider, AgentProviderCapabilities, AgentSession, AgentSessionStartConfig,
    AgentSessionStartHook, AgentSessionStartHookOutput, AgentSessionState, AgentStructuredOutput,
    AgentTextOutput, AgentToolAnnotations, AgentToolConfig, AgentToolConfigSource, AgentToolRef,
    AgentToolSourceMode, AgentTurn, AgentTurnDisplay, AgentTurnEvent, AgentTurnOutput,
    AgentTurnStructuredOutput, AgentTurnTextOutput, AgentWorkspace, AgentWorkspaceGitCheckout,
    CancelAgentProviderTurnRequest, CreateAgentProviderSessionRequest,
    CreateAgentProviderTurnRequest, ExecuteAgentToolResponse, GetAgentProviderCapabilitiesRequest,
    GetAgentProviderInteractionRequest, GetAgentProviderSessionRequest,
    GetAgentProviderTurnRequest, ListAgentProviderInteractionsRequest,
    ListAgentProviderInteractionsResponse, ListAgentProviderSessionsRequest,
    ListAgentProviderSessionsResponse, ListAgentProviderTurnEventsRequest,
    ListAgentProviderTurnEventsResponse, ListAgentProviderTurnsRequest,
    ListAgentProviderTurnsResponse, ListAgentToolsResponse, ListedAgentTool,
    ResolveAgentProviderInteractionRequest, ResolvedAgentConnection, ResolvedAgentTool,
    UpdateAgentProviderSessionRequest, new_agent_image_ref, new_agent_message,
    new_agent_message_part, new_agent_tool_call, new_agent_tool_ref, new_agent_tool_result,
};
pub use agent_access::{
    Agent, AgentCancelTurn, AgentContract, AgentCreateSession, AgentCreateTurn, AgentError,
    AgentGetSession, AgentGetTurn, AgentListInteractions, AgentListInteractionsResponse,
    AgentListSessions, AgentListSessionsResponse, AgentListTurnEvents, AgentListTurnEventsResponse,
    AgentListTurns, AgentListTurnsResponse, AgentResolveInteraction, AgentUpdateSession,
};
pub use api::parse_subject_id;
pub use api::{
    Access, Credential, HTTPSubjectRequest, Host, Provider, Request, Response, RuntimeMetadata,
    Subject, current_request_context, ok, with_request_context,
};
pub use app_access::{App, AppContract, AppError, InvokeGraphQLOptions, InvokeOptions};
pub use app_decode::InvokeError;
pub use auth::{
    AuthSessionSettings, AuthenticatedUser, AuthenticationProvider, BeginLoginRequest,
    BeginLoginResponse, CompleteLoginRequest,
};
pub use cache::{Cache, CacheApi, CacheEntry, CacheError, CacheProvider, CacheSetOptions};
pub use catalog::{Catalog, CatalogOperation, CatalogParameter, OperationAnnotations};
pub use env::{
    CURRENT_PROTOCOL_VERSION, ENV_HOST_SERVICE_SOCKET, ENV_HOST_SERVICE_TOKEN, ENV_PROVIDER_SOCKET,
};
pub use error::{Error, Result};
pub use indexeddb::{
    Cursor, CursorApi, CursorDirection, Index, IndexApi, IndexedDB, IndexedDBApi,
    IndexedDBCursorSnapshot, IndexedDBCursorSnapshotEntry, IndexedDBError,
    IndexedDBOpenCursorRequest, ObjectStore, ObjectStoreApi, Transaction, TransactionApi,
    TransactionDurabilityHint, TransactionIndex, TransactionIndexApi, TransactionMode,
    TransactionObjectStore, TransactionObjectStoreApi, TransactionOptions,
    compare_indexeddb_values, indexeddb_range_bounds, new_indexeddb_cursor_snapshot,
};
#[doc(hidden)]
pub use provider_server::{OperationResult, ProviderServer};
pub use router::{Operation, Router};
pub use runtime_log_host::{
    AppendRuntimeLogsRequest, AppendRuntimeLogsResponse, ENV_RUNTIME_SESSION_ID, RuntimeLogEntry,
    RuntimeLogHost, RuntimeLogHostError, RuntimeLogStream, runtime_session_id,
};
pub use runtime_provider::{
    GetRuntimeSessionRequest, HostedApp, ListRuntimeSessionsRequest, ListRuntimeSessionsResponse,
    PrepareRuntimeWorkspaceRequest, PrepareRuntimeWorkspaceResponse, RemoveRuntimeWorkspaceRequest,
    RuntimeEgressMode, RuntimeImagePullAuth, RuntimeProvider, RuntimeSession,
    RuntimeSessionLifecycle, RuntimeSupport, StartHostedAppRequest, StartRuntimeSessionRequest,
    StopRuntimeSessionRequest,
};
pub use s3::{S3, S3Api, S3Error, S3Object, S3ObjectApi, S3Provider};
pub use s3::{S3ReadObjectFrame, S3ReadObjectStream, S3WriteObjectFrame, S3WriteObjectStream};
pub use secrets::SecretsProvider;
pub use tonic::codegen::async_trait;
pub use workflow::{
    ApplyWorkflowProviderDefinitionRequest, BoundWorkflowTarget, CancelWorkflowProviderRunRequest,
    DeleteWorkflowProviderDefinitionRequest, DeliverWorkflowProviderEventRequest,
    GetWorkflowProviderDefinitionRequest, GetWorkflowProviderRunEventsRequest,
    GetWorkflowProviderRunEventsResponse, GetWorkflowProviderRunOutputRequest,
    GetWorkflowProviderRunOutputResponse, GetWorkflowProviderRunRequest,
    ListWorkflowProviderDefinitionsRequest, ListWorkflowProviderDefinitionsResponse,
    ListWorkflowProviderRunsRequest, ListWorkflowProviderRunsResponse,
    SetWorkflowProviderActivationPausedRequest, SetWorkflowProviderDefinitionPausedRequest,
    SignalOrStartWorkflowProviderRunRequest, SignalWorkflowProviderRunRequest,
    SignalWorkflowRunResponse, StartWorkflowProviderRunRequest, WorkflowActivation,
    WorkflowAgentMessage, WorkflowArray, WorkflowDefinition, WorkflowDefinitionSpec,
    WorkflowEvalContext, WorkflowEvalResult, WorkflowEvent, WorkflowEventActivation,
    WorkflowEventMatch, WorkflowEventTriggerInvocation, WorkflowExecutionRequest, WorkflowJson,
    WorkflowManualTrigger, WorkflowObject, WorkflowPathSource, WorkflowProvider, WorkflowRun,
    WorkflowRunEvent, WorkflowRunStatus, WorkflowRunTrigger, WorkflowScheduleActivation,
    WorkflowScheduleTrigger, WorkflowSignal, WorkflowStep, WorkflowStepAction,
    WorkflowStepAgentTurn, WorkflowStepAppCall, WorkflowStepAttempt, WorkflowStepExecution,
    WorkflowStepInputSource, WorkflowStepOutputSource, WorkflowStepStatus, WorkflowStepWhen,
    WorkflowText, WorkflowValue, evaluate_workflow_value, latest_workflow_signal,
    new_bound_workflow_target, new_bound_workflow_target_from_target, new_workflow_agent_message,
    new_workflow_definition, new_workflow_definition_spec, new_workflow_event,
    new_workflow_event_from_event, new_workflow_event_match, new_workflow_run,
    new_workflow_run_from_run, new_workflow_signal, new_workflow_signal_from_signal,
    new_workflow_step, new_workflow_step_agent_turn, new_workflow_step_app_call,
    new_workflow_step_when, new_workflow_text, new_workflow_value, path_value,
    render_workflow_template, workflow_event_input_from_event,
    workflow_event_match_input_from_match, workflow_run_trigger_input_from_trigger,
    workflow_signal_input_from_signal, workflow_step_agent_turn_input_from_turn,
    workflow_step_app_call_input_from_call, workflow_step_input_from_step,
    workflow_subject_from_proto, workflow_subject_to_proto, workflow_value_array,
    workflow_value_input, workflow_value_input_from_value, workflow_value_literal,
    workflow_value_object, workflow_value_signal, workflow_value_step_input,
    workflow_value_step_output, workflow_value_template,
};
pub use workflow_access::{
    Workflow, WorkflowApplyDefinition, WorkflowCancelRun, WorkflowContract,
    WorkflowDeleteDefinition, WorkflowDeliverEvent, WorkflowError, WorkflowGetDefinition,
    WorkflowGetRun, WorkflowGetRunEvents, WorkflowGetRunOutput, WorkflowListRuns,
    WorkflowSetActivationPaused, WorkflowSetDefinitionPaused, WorkflowSignalOrStartRun,
    WorkflowSignalRun, WorkflowStartRun,
};
#[doc(hidden)]
pub trait IntoRouterResult<P> {
    fn into_router_result(self) -> Result<Router<P>>;
}

impl<P> IntoRouterResult<P> for Router<P> {
    fn into_router_result(self) -> Result<Router<P>> {
        Ok(self)
    }
}

impl<P> IntoRouterResult<P> for Result<Router<P>> {
    fn into_router_result(self) -> Result<Router<P>> {
        self
    }
}

#[doc(hidden)]
/// Converts router-like values used by the export macros into a [`Router`].
pub fn into_router_result<P, R>(router: R) -> Result<Router<P>>
where
    R: IntoRouterResult<P>,
{
    router.into_router_result()
}

/// Exports the integration-provider entrypoints expected by `gestaltd`.
#[macro_export]
macro_rules! export_provider {
    (constructor = $constructor:path, router = $router:path $(,)?) => {
        pub fn __gestalt_serve(name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            let router = $crate::into_router_result($router())?.with_name(name);
            $crate::runtime::run_provider(provider, router)
        }

        pub fn __gestalt_write_catalog(name: &str, path: &str) -> $crate::Result<()> {
            let router = $crate::into_router_result($router())?.with_name(name);
            $crate::runtime::write_catalog_path(&router, path)
        }
    };
}

/// Exports the authentication-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_authentication_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_authentication(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_authentication_provider(provider)
        }
    };
}

/// Exports the cache-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_cache_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_cache(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_cache_provider(provider)
        }
    };
}

/// Exports the secrets-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_secrets_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_secrets(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_secrets_provider(provider)
        }
    };
}

/// Exports the S3-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_s3_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_s3(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_s3_provider(provider)
        }
    };
}

/// Exports the runtime-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_runtime_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_runtime(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_runtime_provider(provider)
        }
    };
}

/// Exports the workflow-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_workflow_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_workflow(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_workflow_provider(provider)
        }
    };
}

/// Exports the agent-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_agent_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_agent(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_agent_provider(provider)
        }
    };
}
