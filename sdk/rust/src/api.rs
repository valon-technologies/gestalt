use std::collections::BTreeMap;
use std::convert::Infallible;
use std::future::Future;

use tonic::codegen::async_trait;

use crate::agent_provider::AgentToolRef;
use crate::catalog::Catalog;
use crate::error::{Error, Result};
use crate::proto::v1;
use crate::rpc_support::GestaltError;

/// Split a canonical subject ID such as `user:ada` into kind and id.
pub fn parse_subject_id(subject_id: &str) -> Option<(&str, &str)> {
    let trimmed = subject_id.trim();
    let (kind, id) = trimmed.split_once(':')?;
    let kind = kind.trim();
    let id = id.trim();
    if kind.is_empty() || id.is_empty() {
        return None;
    }
    Some((kind, id))
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Identifies the caller that initiated an operation.
pub struct Subject {
    /// Stable subject id.
    pub id: String,
    /// Email address resolved by the Gestalt host for user subjects.
    pub email: String,
    /// Human-readable display name resolved by the Gestalt host for user subjects.
    pub display_name: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Describes the resolved credential used to authorize an operation.
pub struct Credential {
    /// Credential mode used by the host.
    pub mode: String,
    /// Subject id associated with the credential.
    pub subject_id: String,
    /// Connection id or name associated with the credential.
    pub connection: String,
    /// Provider instance id or name associated with the credential.
    pub instance: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Summarizes the host-side access decision attached to an operation.
pub struct Access {
    /// Policy name or id applied to the request.
    pub policy: String,
    /// Effective role granted to the request.
    pub role: String,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Describes public host metadata attached to a request.
pub struct Host {
    /// Public base URL for the Gestalt host.
    pub public_base_url: String,
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Carries execution-scoped metadata into typed operation handlers.
pub struct Request {
    /// Request token supplied to hosted HTTP operation handlers.
    pub token: String,
    /// Connection parameters resolved by the host.
    pub connection_params: BTreeMap<String, String>,
    /// Subject that initiated the request.
    pub subject: Subject,
    /// Original agent caller when an agent tool runs as a delegated subject.
    pub agent_subject: Subject,
    /// Credential used to authorize the request.
    pub credential: Credential,
    /// Access decision attached to the request.
    pub access: Access,
    /// Public host metadata attached to the request.
    pub host: Host,
    /// Idempotency key supplied by the host.
    pub idempotency_key: String,
    /// Workflow callback metadata uses a JSON-style lowerCamelCase object
    /// such as `runId`, `target.steps[0].app.name`,
    /// `trigger.activationId`, and `trigger.event.specVersion`.
    pub workflow: serde_json::Map<String, serde_json::Value>,
    /// Agent tool refs granted to the current operation request.
    pub tool_refs: Vec<AgentToolRef>,
    /// Whether the host attached a tool-ref context to this request.
    pub tool_refs_set: bool,
}

tokio::task_local! {
    static REQUEST_CONTEXT: Option<v1::RequestContext>;
}

impl Request {
    /// Returns one resolved connection parameter by name.
    pub fn connection_param(&self, name: &str) -> Option<&str> {
        self.connection_params.get(name).map(String::as_str)
    }

    /// Returns a public Gestalt client bound to the current provider request.
    pub async fn gestalt(
        &self,
    ) -> std::result::Result<crate::public::bound::BoundGestaltClient, GestaltError> {
        crate::public::gestalt_from_context().await
    }
}

/// Returns the host-supplied request context for the currently executing provider handler.
pub fn current_request_context() -> Option<v1::RequestContext> {
    REQUEST_CONTEXT.try_with(Clone::clone).ok().flatten()
}

/// Returns the current ambient request context in the generated clients'
/// native form, for `with_context` on contextful clients such as
/// [`App`](crate::App), [`Agent`](crate::Agent), and [`Workflow`](crate::Workflow).
pub fn current_native_request_context() -> Option<crate::app::RequestContext> {
    current_request_context().map(native_request_context)
}

fn native_request_context(value: v1::RequestContext) -> crate::app::RequestContext {
    use crate::codec::app::{from_wire_agent_tool_ref, from_wire_subject_context};
    use crate::codec::support::from_wire_struct;

    crate::app::RequestContext {
        subject: value.subject.map(from_wire_subject_context),
        credential: value
            .credential
            .map(|credential| crate::app::CredentialContext {
                mode: credential.mode,
                subject_id: credential.subject_id,
                connection: credential.connection,
                instance: credential.instance,
            }),
        access: value.access.map(|access| crate::app::AccessContext {
            policy: access.policy,
            role: access.role,
        }),
        workflow: value.workflow.map(from_wire_struct),
        host: value.host.map(|host| crate::app::HostContext {
            public_base_url: host.public_base_url,
        }),
        agent_subject: value.agent_subject.map(from_wire_subject_context),
        caller: value.caller.map(|caller| crate::app::ProviderContext {
            kind: caller.kind,
            name: caller.name,
        }),
        invocation: value
            .invocation
            .map(|invocation| crate::app::InvocationContext {
                request_id: invocation.request_id,
                depth: invocation.depth,
                call_chain: invocation.call_chain,
                surface: invocation.surface,
                internal_connection_access: invocation.internal_connection_access,
                connection: invocation.connection,
            }),
        tool_refs: value
            .tool_refs
            .into_iter()
            .map(from_wire_agent_tool_ref)
            .collect(),
        tool_refs_set: value.tool_refs_set,
        request_meta: value
            .request_meta
            .map(|meta| crate::app::RequestMetaContext {
                client_ip: meta.client_ip,
                remote_addr: meta.remote_addr,
                user_agent: meta.user_agent,
            }),
        agent: value.agent.map(|agent| crate::app::AgentInvocationContext {
            provider_name: agent.provider_name,
            session_id: agent.session_id,
            turn_id: agent.turn_id,
        }),
    }
}

/// Runs an async operation with the supplied request context available to Gestalt clients.
pub async fn with_request_context<F>(context: Option<v1::RequestContext>, future: F) -> F::Output
where
    F: Future,
{
    REQUEST_CONTEXT.scope(context, future).await
}

pub(crate) async fn scope_request_context<F>(
    context: Option<v1::RequestContext>,
    future: F,
) -> F::Output
where
    F: Future,
{
    with_request_context(context, future).await
}

#[derive(Clone, Debug, Default, PartialEq)]
/// Carries one verified hosted HTTP request into a provider subject resolver.
pub struct HTTPSubjectRequest {
    /// Hosted HTTP binding name from the app manifest.
    pub binding: String,
    /// HTTP method used for the inbound request.
    pub method: String,
    /// Request path received by the hosted HTTP binding.
    pub path: String,
    /// Request content type.
    pub content_type: String,
    /// Request headers after host-side verification.
    pub headers: BTreeMap<String, Vec<String>>,
    /// Request query parameters.
    pub query: BTreeMap<String, Vec<String>>,
    /// Decoded request parameters.
    pub params: serde_json::Map<String, serde_json::Value>,
    /// Raw request body bytes.
    pub raw_body: Vec<u8>,
    /// Security scheme used to verify the request.
    pub security_scheme: String,
    /// Subject string verified by the security scheme, when available.
    pub verified_subject: String,
    /// Claims verified by the security scheme.
    pub verified_claims: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
/// Wraps a typed handler response plus an optional explicit HTTP status code.
pub struct Response<T> {
    /// Optional explicit HTTP-style status code.
    pub status: Option<u16>,
    /// HTTP response headers returned by the handler.
    pub headers: BTreeMap<String, Vec<String>>,
    /// Typed response body returned by the handler.
    pub body: T,
}

impl<T> Response<T> {
    /// Creates a response with an explicit HTTP status code.
    pub fn new(status: u16, body: T) -> Self {
        Self {
            status: Some(status),
            headers: BTreeMap::new(),
            body,
        }
    }

    /// Adds one HTTP response header value.
    pub fn with_header(mut self, name: impl Into<String>, value: impl Into<String>) -> Self {
        self.headers
            .entry(name.into())
            .or_default()
            .push(value.into());
        self
    }
}

/// Returns a successful JSON response with status code `200`.
pub fn ok<T>(body: T) -> Response<T> {
    Response::new(200, body)
}

/// Converts handler return values into a typed [`Response`].
pub trait IntoResponse<T> {
    /// Converts a handler return value into a typed response wrapper.
    fn into_response(self) -> Response<T>;
}

impl<T> IntoResponse<T> for Response<T> {
    fn into_response(self) -> Response<T> {
        self
    }
}

impl<T> IntoResponse<T> for T {
    fn into_response(self) -> Response<T> {
        ok(self)
    }
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
/// Describes provider metadata that should be surfaced by the runtime.
pub struct RuntimeMetadata {
    /// Provider name to report to the host.
    pub name: String,
    /// Human-readable provider display name.
    pub display_name: String,
    /// Human-readable provider description.
    pub description: String,
    /// Provider version string.
    pub version: String,
}

#[async_trait]
/// Shared lifecycle contract for Gestalt integration providers.
pub trait Provider: Send + Sync + 'static {
    /// Configures the provider before it starts serving requests.
    async fn configure(
        &self,
        _name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> Result<()> {
        Ok(())
    }

    /// Returns runtime metadata that should augment the static manifest.
    fn metadata(&self) -> Option<RuntimeMetadata> {
        None
    }

    /// Returns non-fatal warnings the host should surface to users.
    fn warnings(&self) -> Vec<String> {
        Vec::new()
    }

    /// Performs an optional health check.
    async fn health_check(&self) -> Result<()> {
        Ok(())
    }

    /// Starts provider-owned background work after configuration.
    async fn start(&self) -> Result<()> {
        Ok(())
    }

    /// Reports whether this provider can derive additional operations from the
    /// current request context.
    fn supports_session_catalog(&self) -> bool {
        false
    }

    /// Returns workflow definitions declared as static desired state.
    fn workflow_definitions(&self) -> Vec<crate::workflow::WorkflowDefinitionSpec> {
        Vec::new()
    }

    /// Returns an optional request-scoped catalog extension.
    async fn catalog_for_request(&self, _request: &Request) -> Result<Option<Catalog>> {
        Ok(None)
    }

    /// Resolves a hosted HTTP request to a concrete subject before dispatch.
    async fn resolve_http_subject(
        &self,
        _request: HTTPSubjectRequest,
        _context: &Request,
    ) -> Result<Option<Subject>> {
        Ok(None)
    }

    /// Shuts the provider down before the runtime exits.
    async fn close(&self) -> Result<()> {
        Ok(())
    }
}

impl From<Infallible> for Error {
    fn from(_value: Infallible) -> Self {
        Error::internal("unreachable infallible error")
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::protocol;

    #[tokio::test]
    async fn converts_fully_populated_request_context() {
        let wire = v1::RequestContext {
            subject: Some(v1::SubjectContext {
                id: "user:ada".to_string(),
                email: "ada@example.test".to_string(),
                display_name: "Ada".to_string(),
                scopes: vec!["repo:read".to_string()],
                permissions: vec![v1::SubjectPermissionContext {
                    app: "github".to_string(),
                    operations: vec!["issues.get".to_string()],
                    all_operations: false,
                }],
            }),
            credential: Some(v1::CredentialContext {
                mode: "user".to_string(),
                subject_id: "user:cred".to_string(),
                connection: "work".to_string(),
                instance: "primary".to_string(),
            }),
            access: Some(v1::AccessContext {
                policy: "default".to_string(),
                role: "admin".to_string(),
            }),
            workflow: Some(
                protocol::struct_from_json(serde_json::json!({
                    "runId": "run-1",
                    "trigger": { "activationId": "act-1" },
                }))
                .expect("workflow struct"),
            ),
            host: Some(v1::HostContext {
                public_base_url: "https://gestalt.example.test".to_string(),
            }),
            agent_subject: Some(v1::SubjectContext {
                id: "agent:caller".to_string(),
                ..Default::default()
            }),
            caller: Some(v1::ProviderContext {
                kind: "app".to_string(),
                name: "hermes".to_string(),
            }),
            invocation: Some(v1::InvocationContext {
                request_id: "req-1".to_string(),
                depth: 2,
                call_chain: vec!["hermes".to_string(), "github".to_string()],
                surface: "mcp".to_string(),
                internal_connection_access: true,
                connection: "work".to_string(),
            }),
            tool_refs: vec![v1::AgentToolRef {
                app: "slack".to_string(),
                operation: "chat.postMessage".to_string(),
                connection: "workspace".to_string(),
                instance: "primary".to_string(),
                title: "Send Slack message".to_string(),
                description: "Post a Slack message".to_string(),
                credential_mode: "user".to_string(),
                system: "slack".to_string(),
                run_as: Some(v1::SubjectContext {
                    id: "user:run-as".to_string(),
                    ..Default::default()
                }),
            }],
            tool_refs_set: true,
            request_meta: Some(v1::RequestMetaContext {
                client_ip: "203.0.113.7".to_string(),
                remote_addr: "203.0.113.7:443".to_string(),
                user_agent: "gestalt-test".to_string(),
            }),
            agent: Some(v1::AgentInvocationContext {
                provider_name: "openai".to_string(),
                session_id: "session-1".to_string(),
                turn_id: "turn-1".to_string(),
            }),
        };

        let native = with_request_context(Some(wire), async { current_native_request_context() })
            .await
            .expect("native context");

        assert_eq!(
            native,
            crate::app::RequestContext {
                subject: Some(crate::app::SubjectContext {
                    id: "user:ada".to_string(),
                    email: "ada@example.test".to_string(),
                    display_name: "Ada".to_string(),
                    scopes: vec!["repo:read".to_string()],
                    permissions: vec![crate::app::SubjectPermissionContext {
                        app: "github".to_string(),
                        operations: vec!["issues.get".to_string()],
                        all_operations: false,
                    }],
                }),
                credential: Some(crate::app::CredentialContext {
                    mode: "user".to_string(),
                    subject_id: "user:cred".to_string(),
                    connection: "work".to_string(),
                    instance: "primary".to_string(),
                }),
                access: Some(crate::app::AccessContext {
                    policy: "default".to_string(),
                    role: "admin".to_string(),
                }),
                workflow: serde_json::json!({
                    "runId": "run-1",
                    "trigger": { "activationId": "act-1" },
                })
                .as_object()
                .cloned(),
                host: Some(crate::app::HostContext {
                    public_base_url: "https://gestalt.example.test".to_string(),
                }),
                agent_subject: Some(crate::app::SubjectContext {
                    id: "agent:caller".to_string(),
                    ..Default::default()
                }),
                caller: Some(crate::app::ProviderContext {
                    kind: "app".to_string(),
                    name: "hermes".to_string(),
                }),
                invocation: Some(crate::app::InvocationContext {
                    request_id: "req-1".to_string(),
                    depth: 2,
                    call_chain: vec!["hermes".to_string(), "github".to_string()],
                    surface: "mcp".to_string(),
                    internal_connection_access: true,
                    connection: "work".to_string(),
                }),
                tool_refs: vec![crate::app::AgentToolRef {
                    app: "slack".to_string(),
                    operation: "chat.postMessage".to_string(),
                    connection: "workspace".to_string(),
                    instance: "primary".to_string(),
                    title: "Send Slack message".to_string(),
                    description: "Post a Slack message".to_string(),
                    credential_mode: "user".to_string(),
                    system: "slack".to_string(),
                    run_as: Some(crate::app::SubjectContext {
                        id: "user:run-as".to_string(),
                        ..Default::default()
                    }),
                }],
                tool_refs_set: true,
                request_meta: Some(crate::app::RequestMetaContext {
                    client_ip: "203.0.113.7".to_string(),
                    remote_addr: "203.0.113.7:443".to_string(),
                    user_agent: "gestalt-test".to_string(),
                }),
                agent: Some(crate::app::AgentInvocationContext {
                    provider_name: "openai".to_string(),
                    session_id: "session-1".to_string(),
                    turn_id: "turn-1".to_string(),
                }),
            }
        );
    }

    #[tokio::test]
    async fn converts_sparse_request_context() {
        let native = with_request_context(Some(v1::RequestContext::default()), async {
            current_native_request_context()
        })
        .await
        .expect("native context");

        assert_eq!(native, crate::app::RequestContext::default());
    }

    #[test]
    fn returns_none_outside_request_scope() {
        assert!(current_native_request_context().is_none());
    }
}
