#![warn(rustdoc::broken_intra_doc_links)]
#![doc = include_str!("../README.md")]

mod agent;
mod agent_manager;
mod api;
mod auth;
mod auth_server;
mod authorization;
mod cache;
mod cache_server;
mod catalog;
mod env;
mod error;
mod generated;
/// IndexedDB-style datastore client and provider helpers.
pub mod indexeddb;
mod invoker;
mod plugin_runtime;
mod protocol;
mod provider_server;
mod router;
mod rpc_status;
/// Runtime entrypoints for serving Gestalt provider surfaces over Unix sockets.
pub mod runtime;
mod runtime_log_host;
mod runtime_server;
/// S3-compatible client and provider helpers.
pub mod s3;
mod secrets;
mod secrets_server;
/// OpenTelemetry helpers for provider-authored GenAI instrumentation.
pub mod telemetry;
mod workflow;
mod workflow_manager;

#[doc(hidden)]
pub mod proto {
    pub use crate::generated::v1;
}

pub use agent::{
    AgentActor, AgentExecutionStatus, AgentHost, AgentHostError, AgentHostExecuteToolInput,
    AgentHostListToolsInput, AgentHostResolveConnectionInput, AgentInteraction,
    AgentInteractionState, AgentInteractionType, AgentJson, AgentMessage, AgentMessagePart,
    AgentMessagePartImageRef, AgentMessagePartToolCall, AgentMessagePartToolResult,
    AgentMessagePartType, AgentPreparedWorkspace, AgentProvider, AgentProviderCapabilities,
    AgentSession, AgentSessionStartConfig, AgentSessionStartHook, AgentSessionStartHookOutput,
    AgentSessionState, AgentToolAnnotations, AgentToolRef, AgentToolSourceMode, AgentTurn,
    AgentTurnDisplay, AgentTurnEvent, AgentWorkspace, AgentWorkspaceGitCheckout,
    CancelAgentProviderTurnRequest, CreateAgentProviderSessionRequest,
    CreateAgentProviderTurnRequest, ENV_AGENT_HOST_SOCKET, ExecuteAgentToolResponse,
    GetAgentProviderCapabilitiesRequest, GetAgentProviderInteractionRequest,
    GetAgentProviderSessionRequest, GetAgentProviderTurnRequest,
    ListAgentProviderInteractionsRequest, ListAgentProviderInteractionsResponse,
    ListAgentProviderSessionsRequest, ListAgentProviderSessionsResponse,
    ListAgentProviderTurnEventsRequest, ListAgentProviderTurnEventsResponse,
    ListAgentProviderTurnsRequest, ListAgentProviderTurnsResponse, ListAgentToolsResponse,
    ListedAgentTool, ResolveAgentProviderInteractionRequest, ResolvedAgentConnection,
    ResolvedAgentTool, UpdateAgentProviderSessionRequest, new_agent_image_ref, new_agent_message,
    new_agent_message_part, new_agent_tool_call, new_agent_tool_ref, new_agent_tool_result,
};
pub use agent_manager::{
    AgentManager, AgentManagerCancelTurn, AgentManagerCreateSession, AgentManagerCreateTurn,
    AgentManagerError, AgentManagerGetSession, AgentManagerGetTurn, AgentManagerListInteractions,
    AgentManagerListInteractionsResponse, AgentManagerListSessions,
    AgentManagerListSessionsResponse, AgentManagerListTurnEvents,
    AgentManagerListTurnEventsResponse, AgentManagerListTurns, AgentManagerListTurnsResponse,
    AgentManagerResolveInteraction, AgentManagerUpdateSession, ENV_AGENT_MANAGER_SOCKET,
};
pub use api::{
    Access, ConnectedToken, Credential, ExternalIdentity, HTTPSubjectRequest, Host, Provider,
    Request, Response, RuntimeMetadata, Subject, ok,
};
pub use auth::{
    AuthSessionSettings, AuthenticatedUser, AuthenticationProvider, BeginLoginRequest,
    BeginLoginResponse, CompleteLoginRequest,
};
pub use authorization::{
    AUTHORIZATION_SUBJECT_TYPE_SUBJECT, AccessDecision, AccessEvaluationRequest,
    AccessEvaluationsRequest, AccessEvaluationsResponse, ActionSearchRequest, ActionSearchResponse,
    Authorization, AuthorizationAction, AuthorizationError, AuthorizationMetadata,
    AuthorizationModel, AuthorizationModelAction, AuthorizationModelAllowedTarget,
    AuthorizationModelComputedUserset, AuthorizationModelRef, AuthorizationModelRelation,
    AuthorizationModelResourceType, AuthorizationModelRewrite, AuthorizationModelRewriteThis,
    AuthorizationModelRewriteUnion, AuthorizationModelSubjectSetTarget,
    AuthorizationModelTupleToUserset, AuthorizationProvider, AuthorizationRelationshipTarget,
    AuthorizationResource, AuthorizationSubject, AuthorizationSubjectSet, ENV_AUTHORIZATION_SOCKET,
    EffectiveSubjectSearchRequest, EffectiveSubjectSearchResponse, ExpandNode, ExpandRequest,
    ExpandResponse, GetActiveModelResponse, ListModelsRequest, ListModelsResponse,
    ReadRelationshipsRequest, ReadRelationshipsResponse, Relationship, RelationshipKey,
    ResourceSearchRequest, ResourceSearchResponse, SubjectSearchRequest, SubjectSearchResponse,
    WriteModelRequest, WriteRelationshipsRequest,
};
pub use cache::{
    Cache, CacheEntry, CacheError, CacheProvider, CacheSetOptions, ENV_CACHE_SOCKET,
    cache_socket_env, cache_socket_token_env,
};
pub use catalog::{Catalog, CatalogOperation, CatalogParameter, OperationAnnotations};
pub use env::{CURRENT_PROTOCOL_VERSION, ENV_PROVIDER_SOCKET};
pub use error::{Error, Result};
pub use indexeddb::{
    Cursor, CursorDirection, ENV_INDEXEDDB_SOCKET, IndexedDB, IndexedDBCursorSnapshot,
    IndexedDBCursorSnapshotEntry, IndexedDBError, IndexedDBOpenCursorRequest, Transaction,
    TransactionDurabilityHint, TransactionIndexClient, TransactionMode, TransactionObjectStore,
    TransactionOptions, compare_indexeddb_values, indexeddb_range_bounds, indexeddb_socket_env,
    indexeddb_socket_token_env, new_indexeddb_cursor_snapshot,
};
pub use invoker::{
    ENV_PLUGIN_INVOKER_SOCKET, InvocationGrant, InvokeOptions, PluginInvoker, PluginInvokerError,
};
pub use plugin_runtime::{
    GetPluginRuntimeSessionRequest, HostedPlugin, ListPluginRuntimeSessionsRequest,
    ListPluginRuntimeSessionsResponse, PluginRuntimeEgressMode, PluginRuntimeImagePullAuth,
    PluginRuntimeProvider, PluginRuntimeSession, PluginRuntimeSessionLifecycle,
    PluginRuntimeSupport, PreparePluginRuntimeWorkspaceRequest,
    PreparePluginRuntimeWorkspaceResponse, RemovePluginRuntimeWorkspaceRequest,
    StartHostedPluginRequest, StartPluginRuntimeSessionRequest, StopPluginRuntimeSessionRequest,
};
#[doc(hidden)]
pub use provider_server::{OperationResult, ProviderServer};
pub use router::{Operation, Router};
pub use runtime_log_host::{
    AppendPluginRuntimeLogsRequest, AppendPluginRuntimeLogsResponse, ENV_RUNTIME_LOG_HOST_SOCKET,
    ENV_RUNTIME_SESSION_ID, PluginRuntimeLogEntry, RuntimeLogHost, RuntimeLogHostError,
    RuntimeLogStream, runtime_session_id,
};
pub use s3::{ENV_S3_SOCKET, S3, S3Error, S3Provider, s3_socket_env, s3_socket_token_env};
pub use s3::{S3ReadObjectFrame, S3ReadObjectStream, S3WriteObjectFrame, S3WriteObjectStream};
pub use secrets::SecretsProvider;
pub use tonic::codegen::async_trait;
pub use workflow::{
    BoundWorkflowAgentTarget, BoundWorkflowDefinition, BoundWorkflowEventTrigger,
    BoundWorkflowPluginTarget, BoundWorkflowRun, BoundWorkflowSchedule, BoundWorkflowTarget,
    CancelWorkflowProviderRunRequest, DeleteWorkflowProviderEventTriggerRequest,
    DeleteWorkflowProviderScheduleRequest, ENV_WORKFLOW_HOST_SOCKET,
    GetWorkflowExecutionReferenceRequest, GetWorkflowProviderEventTriggerRequest,
    GetWorkflowProviderRunRequest, GetWorkflowProviderScheduleRequest,
    InvokeWorkflowOperationInput, InvokeWorkflowOperationResponse,
    ListWorkflowExecutionReferencesRequest, ListWorkflowExecutionReferencesResponse,
    ListWorkflowProviderEventTriggersRequest, ListWorkflowProviderEventTriggersResponse,
    ListWorkflowProviderRunsRequest, ListWorkflowProviderRunsResponse,
    ListWorkflowProviderSchedulesRequest, ListWorkflowProviderSchedulesResponse,
    ManagedWorkflowDefinition, ManagedWorkflowEventTrigger, ManagedWorkflowRun,
    ManagedWorkflowRunSignal, ManagedWorkflowSchedule, PauseWorkflowProviderEventTriggerRequest,
    PauseWorkflowProviderScheduleRequest, PublishWorkflowProviderEventRequest,
    PutWorkflowExecutionReferenceRequest, ResumeWorkflowProviderEventTriggerRequest,
    ResumeWorkflowProviderScheduleRequest, SignalOrStartWorkflowProviderRunRequest,
    SignalWorkflowProviderRunRequest, SignalWorkflowRunResponse, StartWorkflowProviderRunRequest,
    UpsertWorkflowProviderEventTriggerRequest, UpsertWorkflowProviderScheduleRequest,
    WorkflowAccessPermission, WorkflowActor, WorkflowAgentStep, WorkflowAgentStepWhen,
    WorkflowEvent, WorkflowEventMatch, WorkflowEventTriggerInvocation, WorkflowExecutionReference,
    WorkflowHost, WorkflowHostError, WorkflowJson, WorkflowOutputBinding, WorkflowOutputDelivery,
    WorkflowOutputValueSource, WorkflowProvider, WorkflowRunAsSubject, WorkflowRunStatus,
    WorkflowRunTrigger, WorkflowScheduleTrigger, WorkflowSignal,
    bound_workflow_agent_target_input_from_target, bound_workflow_definition_input_from_definition,
    bound_workflow_event_trigger_input_from_trigger,
    bound_workflow_plugin_target_input_from_target, bound_workflow_run_input_from_run,
    bound_workflow_schedule_input_from_schedule, bound_workflow_target_input_from_target,
    new_bound_workflow_agent_target, new_bound_workflow_event_trigger,
    new_bound_workflow_event_trigger_from_trigger, new_bound_workflow_plugin_target,
    new_bound_workflow_run, new_bound_workflow_run_from_run, new_bound_workflow_schedule,
    new_bound_workflow_schedule_from_schedule, new_bound_workflow_target,
    new_bound_workflow_target_from_target, new_workflow_access_permission, new_workflow_actor,
    new_workflow_event, new_workflow_event_from_event, new_workflow_event_match,
    new_workflow_event_trigger_invocation, new_workflow_execution_reference,
    new_workflow_execution_reference_from_reference, new_workflow_output_binding,
    new_workflow_output_delivery, new_workflow_output_value_source, new_workflow_run_as_subject,
    new_workflow_run_trigger, new_workflow_run_trigger_from_trigger, new_workflow_schedule_trigger,
    new_workflow_signal, new_workflow_signal_from_signal,
    workflow_access_permission_input_from_permission, workflow_actor_input_from_actor,
    workflow_event_input_from_event, workflow_event_match_input_from_match,
    workflow_execution_reference_input_from_reference, workflow_output_binding_input_from_binding,
    workflow_output_delivery_input_from_delivery, workflow_output_value_source_input_from_source,
    workflow_run_as_subject_input_from_subject, workflow_run_trigger_input_from_trigger,
    workflow_signal_input_from_signal,
};
pub use workflow_manager::{
    ENV_WORKFLOW_MANAGER_SOCKET, WorkflowManager, WorkflowManagerCreateDefinition,
    WorkflowManagerCreateEventTrigger, WorkflowManagerCreateSchedule,
    WorkflowManagerDeleteDefinition, WorkflowManagerDeleteEventTrigger,
    WorkflowManagerDeleteSchedule, WorkflowManagerError, WorkflowManagerGetDefinition,
    WorkflowManagerGetEventTrigger, WorkflowManagerGetSchedule, WorkflowManagerPauseEventTrigger,
    WorkflowManagerPauseSchedule, WorkflowManagerPublishEvent, WorkflowManagerResumeEventTrigger,
    WorkflowManagerResumeSchedule, WorkflowManagerSignalOrStartRun, WorkflowManagerSignalRun,
    WorkflowManagerStartRun, WorkflowManagerUpdateDefinition, WorkflowManagerUpdateEventTrigger,
    WorkflowManagerUpdateSchedule,
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

/// Exports the authorization-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_authorization_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_authorization(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_authorization_provider(provider)
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

/// Exports the plugin-runtime-provider entrypoint expected by `gestaltd`.
#[macro_export]
macro_rules! export_plugin_runtime_provider {
    (constructor = $constructor:path $(,)?) => {
        pub fn __gestalt_serve_runtime(_name: &str) -> $crate::Result<()> {
            let provider = std::sync::Arc::new($constructor());
            $crate::runtime::run_plugin_runtime_provider(provider)
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
