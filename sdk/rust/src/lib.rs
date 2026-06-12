#![warn(rustdoc::broken_intra_doc_links)]
#![doc = include_str!("../README.md")]

/// Generated Agent client and native types.
pub mod agent;
mod agent_provider;
mod api;
/// Generated App and AppProvider client and native types.
pub mod app;
/// Generated shared runtime for the sdkgen clients.
mod auth;
mod auth_server;
/// Generated Authentication client and native types.
pub mod authentication;
/// Generated Authorization client and native types.
pub mod authorization;
/// Generated Cache client and native types.
pub mod cache;
mod cache_provider;
mod cache_server;
mod catalog;
mod codec;
mod env;
mod error;
/// Generated ExternalCredentials client and native types.
pub mod external_credential;
mod generated;
/// Generated IndexedDB client and native types.
pub mod indexeddb;
mod indexeddb_provider;
pub mod invoke_support;
mod protocol;
mod provider_server;
mod router;
mod rpc_status;
pub mod rpc_support;
/// Generated ProviderLifecycle client and native types.
pub mod runtime;
/// Runtime entrypoints for serving Gestalt provider surfaces over Unix sockets.
pub mod runtime_impl;
mod runtime_log_host;
/// Generated Runtime and RuntimeLogHost client and native types.
pub mod runtime_provider;
mod runtime_provider_impl;
mod runtime_server;
/// Generated S3 client and native types.
pub mod s3;
/// S3-compatible provider contract and native provider types.
pub mod s3_provider;
/// Generated Secrets client and native types.
pub mod secrets;
mod secrets_provider;
mod secrets_server;
/// OpenTelemetry helpers for provider-authored GenAI instrumentation.
pub mod telemetry;
/// Generated Test client and native types.
pub mod test;
/// Generated Workflow client and native types.
pub mod workflow;
/// Workflow provider contract and native workflow types.
pub mod workflow_provider;

#[doc(hidden)]
pub mod proto {
    pub use crate::generated::v1;
}

pub use agent::Agent;
pub use agent_provider::{
    AgentCatalogToolConfig, AgentExecutionStatus, AgentInteraction, AgentInteractionState,
    AgentInteractionType, AgentJson, AgentMessage, AgentMessagePart, AgentMessagePartImageRef,
    AgentMessagePartToolCall, AgentMessagePartToolResult, AgentMessagePartType, AgentOutput,
    AgentPreparedWorkspace, AgentProvider, AgentProviderCapabilities, AgentSession,
    AgentSessionStartConfig, AgentSessionStartHook, AgentSessionStartHookOutput, AgentSessionState,
    AgentStructuredOutput, AgentTextOutput, AgentToolAnnotations, AgentToolConfig,
    AgentToolConfigSource, AgentToolRef, AgentToolSourceMode, AgentTurn, AgentTurnDisplay,
    AgentTurnEvent, AgentTurnOutput, AgentTurnStructuredOutput, AgentTurnTextOutput,
    AgentWorkspace, AgentWorkspaceGitCheckout, CancelAgentProviderTurnRequest,
    CreateAgentProviderSessionRequest, CreateAgentProviderTurnRequest,
    GetAgentProviderCapabilitiesRequest, GetAgentProviderInteractionRequest,
    GetAgentProviderSessionRequest, GetAgentProviderTurnRequest,
    ListAgentProviderInteractionsRequest, ListAgentProviderInteractionsResponse,
    ListAgentProviderSessionsRequest, ListAgentProviderSessionsResponse,
    ListAgentProviderTurnEventsRequest, ListAgentProviderTurnEventsResponse,
    ListAgentProviderTurnsRequest, ListAgentProviderTurnsResponse, ListedAgentTool,
    ResolveAgentProviderInteractionRequest, UpdateAgentProviderSessionRequest, new_agent_image_ref,
    new_agent_message, new_agent_message_part, new_agent_tool_call, new_agent_tool_ref,
    new_agent_tool_result,
};
pub use api::parse_subject_id;
pub use api::{
    Access, Credential, HTTPSubjectRequest, Host, Provider, Request, Response, RuntimeMetadata,
    Subject, current_native_request_context, current_request_context, ok, with_request_context,
};
pub use app::App;
pub use auth::{
    AuthSessionSettings, AuthenticatedUser, AuthenticationProvider, BeginLoginRequest,
    BeginLoginResponse, CompleteLoginRequest,
};
pub use cache::Cache;
pub use cache_provider::{CacheEntry, CacheProvider, CacheSetOptions};
pub use catalog::{Catalog, CatalogOperation, CatalogParameter, OperationAnnotations};
pub use env::{
    CURRENT_PROTOCOL_VERSION, ENV_HOST_SERVICE_SOCKET, ENV_HOST_SERVICE_TOKEN, ENV_PROVIDER_SOCKET,
};
pub use error::{Error, Result};
pub use indexeddb_provider::{
    Cursor, CursorApi, CursorDirection, Index, IndexApi, IndexSchema, IndexedDB, IndexedDBApi,
    IndexedDBCursorSnapshot, IndexedDBCursorSnapshotEntry, IndexedDBError,
    IndexedDBOpenCursorRequest, KeyRange, ObjectStore, ObjectStoreApi, ObjectStoreSchema, Record,
    Transaction, TransactionApi, TransactionDurabilityHint, TransactionIndex, TransactionIndexApi,
    TransactionMode, TransactionObjectStore, TransactionObjectStoreApi, TransactionOptions,
    compare_indexeddb_values, indexeddb_range_bounds, new_indexeddb_cursor_snapshot,
};
pub use invoke_support::{
    InvokeError, InvokeResultError, decode_app_result, decode_graphql_result, error_for_status,
    is_success,
};
#[doc(hidden)]
pub use provider_server::{OperationResult, ProviderServer};
pub use router::{Operation, Router};
pub use runtime_log_host::{
    AppendRuntimeLogsRequest, AppendRuntimeLogsResponse, ENV_RUNTIME_SESSION_ID, RuntimeLogEntry,
    RuntimeLogHost, RuntimeLogHostError, RuntimeLogStream, runtime_session_id,
};
pub use runtime_provider_impl::{
    GetRuntimeSessionRequest, HostedApp, ListRuntimeSessionsRequest, ListRuntimeSessionsResponse,
    PrepareRuntimeWorkspaceRequest, PrepareRuntimeWorkspaceResponse, RemoveRuntimeWorkspaceRequest,
    RuntimeEgressMode, RuntimeImagePullAuth, RuntimeProvider, RuntimeSession,
    RuntimeSessionLifecycle, RuntimeSupport, StartHostedAppRequest, StartRuntimeSessionRequest,
    StopRuntimeSessionRequest,
};
pub use s3::S3;
pub use s3_provider::{
    S3Provider, S3ReadObjectFrame, S3ReadObjectStream, S3WriteObjectFrame, S3WriteObjectStream,
};
pub use secrets_provider::SecretsProvider;
pub use tonic::codegen::async_trait;
pub use workflow::Workflow;
pub use workflow_provider::{
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
    workflow_step_app_call_input_from_call, workflow_step_input_from_step, workflow_value_array,
    workflow_value_input, workflow_value_input_from_value, workflow_value_literal,
    workflow_value_object, workflow_value_signal, workflow_value_step_input,
    workflow_value_step_output, workflow_value_template,
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
            $crate::runtime_impl::run_provider(provider, router)
        }

        pub fn __gestalt_write_catalog(name: &str, path: &str) -> $crate::Result<()> {
            let router = $crate::into_router_result($router())?.with_name(name);
            $crate::runtime_impl::write_catalog_path(&router, path)
        }
    };
}

/// Exports the authentication-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_authentication_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_authentication(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime_impl::run_authentication_provider(provider)
        }
    };
}

/// Exports the cache-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_cache_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_cache(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime_impl::run_cache_provider(provider)
        }
    };
}

/// Exports the secrets-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_secrets_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_secrets(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime_impl::run_secrets_provider(provider)
        }
    };
}

/// Exports the S3-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_s3_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_s3(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime_impl::run_s3_provider(provider)
        }
    };
}

/// Exports the runtime-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_runtime_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_runtime(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime_impl::run_runtime_provider(provider)
        }
    };
}

/// Exports the workflow-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_workflow_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_workflow(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime_impl::run_workflow_provider(provider)
        }
    };
}

/// Exports the agent-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_agent_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_agent(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime_impl::run_agent_provider(provider)
        }
    };
}
