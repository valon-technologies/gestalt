#[path = "../src/generated.rs"]
mod generated;

#[allow(dead_code)]
mod helpers;

use std::collections::BTreeMap;
use std::sync::{Arc, Mutex};
use std::time::{SystemTime, UNIX_EPOCH};

use generated::v1::app_provider_client::AppProviderClient;
use generated::v1::{
    AccessContext, AgentToolRef, CredentialContext, ExecuteRequest, ExternalIdentityContext,
    GetSessionCatalogRequest, HostContext, HttpSubjectRequest, PostConnectCredential,
    PostConnectRequest, RequestContext, ResolveHttpSubjectRequest, StartProviderRequest,
    StringList, SubjectContext,
};
use gestalt::{Catalog, CatalogOperation, Operation, Provider, Request, Response, Router, ok};
use hyper_util::rt::tokio::TokioIo;
use prost_types::Timestamp;
use tokio::net::UnixStream;
use tonic::Code;
use tonic::codegen::async_trait;
use tonic::transport::Endpoint;
use tower::service_fn;

#[derive(Default)]
struct TestProvider {
    greeting: Mutex<String>,
}

#[async_trait]
impl Provider for TestProvider {
    async fn configure(
        &self,
        _name: &str,
        config: serde_json::Map<String, serde_json::Value>,
    ) -> gestalt::Result<()> {
        let greeting = config
            .get("greeting")
            .and_then(serde_json::Value::as_str)
            .unwrap_or("Hello")
            .to_string();
        *self.greeting.lock().expect("lock greeting") = greeting;
        Ok(())
    }

    fn supports_session_catalog(&self) -> bool {
        true
    }

    async fn catalog_for_request(&self, request: &Request) -> gestalt::Result<Option<Catalog>> {
        Ok(Some(Catalog {
            name: "session-example".to_string(),
            display_name: format!(
                "{}|{}|{}|{}|{}|{}|{}",
                request.connection_param("tenant").unwrap_or_default(),
                request.subject.id,
                request.subject.email,
                request.credential.mode,
                request.access.role,
                request.host.public_base_url,
                request
                    .workflow
                    .get("trigger")
                    .and_then(serde_json::Value::as_object)
                    .and_then(|trigger| trigger.get("kind"))
                    .and_then(serde_json::Value::as_str)
                    .unwrap_or_default(),
            ),
            description: String::new(),
            icon_svg: String::new(),
            operations: vec![CatalogOperation {
                id: "private_echo".to_string(),
                method: "POST".to_string(),
                title: String::new(),
                description: String::new(),
                input_schema: String::new(),
                output_schema: String::new(),
                annotations: None,
                parameters: Vec::new(),
                required_scopes: Vec::new(),
                tags: Vec::new(),
                read_only: false,
                visible: None,
                transport: String::new(),
                allowed_roles: Vec::new(),
            }],
        }))
    }

    async fn resolve_http_subject(
        &self,
        request: gestalt::HTTPSubjectRequest,
        context: &Request,
    ) -> gestalt::Result<Option<gestalt::Subject>> {
        match request.binding.as_str() {
            "command" => {
                let team_id = request
                    .params
                    .get("team_id")
                    .and_then(serde_json::Value::as_str)
                    .unwrap_or_default();
                let user_id = request
                    .params
                    .get("user_id")
                    .and_then(serde_json::Value::as_str)
                    .unwrap_or_default();
                Ok(Some(gestalt::Subject {
                    id: format!("slack:{team_id}:{user_id}"),
                    kind: "user".to_string(),
                    credential_subject_id: String::new(),
                    display_name: format!(
                        "{}|{}|{}|{}|{}|{}|{}|{}|{}|{}|{}|{}|{}|{}|{}|{}",
                        request.method.as_str(),
                        request.path.as_str(),
                        request.content_type.as_str(),
                        request
                            .headers
                            .get("x-slack-signature")
                            .and_then(|values| values.first())
                            .map(String::as_str)
                            .unwrap_or_default(),
                        request
                            .query
                            .get("trace")
                            .and_then(|values| values.first())
                            .map(String::as_str)
                            .unwrap_or_default(),
                        String::from_utf8_lossy(&request.raw_body),
                        request.security_scheme.as_str(),
                        request.verified_subject.as_str(),
                        request
                            .verified_claims
                            .get("team")
                            .map(String::as_str)
                            .unwrap_or_default(),
                        context.subject.email.as_str(),
                        context.agent_subject.email.as_str(),
                        context.external_identity.id.as_str(),
                        context.agent_external_identity.id.as_str(),
                        context.credential.mode.as_str(),
                        context.access.role.as_str(),
                        context.host.public_base_url.as_str(),
                    ),
                    auth_source: context
                        .workflow
                        .get("runId")
                        .and_then(serde_json::Value::as_str)
                        .unwrap_or_default()
                        .to_string(),
                    ..Default::default()
                }))
            }
            "none" => Ok(None),
            "reject" => Err(gestalt::Error::permission_denied("unmapped slack subject")),
            "boom" => Err(gestalt::Error::new("boom")),
            "defaults" => Ok(Some(gestalt::Subject {
                id: format!(
                    "defaults:{}:{}",
                    request.binding.as_str(),
                    context.subject.id.as_str()
                ),
                kind: "system".to_string(),
                ..Default::default()
            })),
            _ => Ok(None),
        }
    }

    fn supports_post_connect(&self) -> bool {
        true
    }

    async fn post_connect(
        &self,
        token: &gestalt::ConnectedToken,
    ) -> gestalt::Result<BTreeMap<String, String>> {
        Ok([
            ("id".to_string(), token.id.clone()),
            ("subject_id".to_string(), token.subject_id.clone()),
            ("integration".to_string(), token.integration.clone()),
            ("connection".to_string(), token.connection.clone()),
            ("instance".to_string(), token.instance.clone()),
            (
                "access_token_len".to_string(),
                token.access_token.len().to_string(),
            ),
            (
                "refresh_token_present".to_string(),
                (!token.refresh_token.is_empty()).to_string(),
            ),
            ("scopes".to_string(), token.scopes.clone()),
            (
                "expires_at".to_string(),
                unix_seconds(token.expires_at.as_ref()),
            ),
            (
                "last_refreshed_at".to_string(),
                unix_seconds(token.last_refreshed_at.as_ref()),
            ),
            (
                "refresh_error_count".to_string(),
                token.refresh_error_count.to_string(),
            ),
            ("metadata_json".to_string(), token.metadata_json.clone()),
            (
                "metadata_count".to_string(),
                token.metadata.len().to_string(),
            ),
            (
                "team_id".to_string(),
                token.metadata.get("team_id").cloned().unwrap_or_default(),
            ),
            (
                "has_count".to_string(),
                token.metadata.contains_key("count").to_string(),
            ),
            (
                "created_at".to_string(),
                unix_seconds(token.created_at.as_ref()),
            ),
            (
                "updated_at".to_string(),
                unix_seconds(token.updated_at.as_ref()),
            ),
        ]
        .into_iter()
        .collect())
    }
}

fn unix_seconds(value: Option<&SystemTime>) -> String {
    value
        .and_then(|value| value.duration_since(UNIX_EPOCH).ok())
        .map(|duration| duration.as_secs().to_string())
        .unwrap_or_default()
}

struct PlainProvider;

#[async_trait]
impl Provider for PlainProvider {}

#[derive(serde::Deserialize, schemars::JsonSchema)]
struct Input {
    name: String,
}

#[derive(serde::Serialize, schemars::JsonSchema)]
struct Output {
    message: String,
    subject_id: String,
    subject_email: String,
    agent_subject_email: String,
    credential_mode: String,
    access_role: String,
    host_base_url: String,
    invocation_token: String,
    idempotency_key: String,
    workflow_run_id: String,
    workflow_trigger_id: String,
    workflow_event_spec_version: String,
    workflow_event_data_content_type: String,
    workflow_created_by_subject_id: String,
    tool_refs_set: bool,
    tool_ref_plugin: String,
    tool_ref_operation: String,
    tool_ref_run_as: String,
    tool_ref_external_id: String,
}

#[tokio::test]
async fn serves_provider_requests_over_unix_socket() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("gestalt-rust-sdk.sock");
    let _socket_guard = helpers::EnvGuard::set(gestalt::ENV_PROVIDER_SOCKET, socket.as_os_str());

    let router = Router::new()
        .register(
            Operation::<Input, Output>::new("greet"),
            |provider: Arc<TestProvider>, input: Input, request: Request| async move {
                let greeting = provider.greeting.lock().expect("lock greeting").clone();
                let subject_id = request.subject.id.clone();
                let subject_email = request.subject.email.clone();
                let agent_subject_email = request.agent_subject.email.clone();
                let credential_mode = request.credential.mode.clone();
                let access_role = request.access.role.clone();
                let host_base_url = request.host.public_base_url.clone();
                let invocation_token = request.invocation_token().to_string();
                Ok::<Response<Output>, std::convert::Infallible>(ok(Output {
                    message: format!("{greeting}, {}!", input.name),
                    subject_id,
                    subject_email,
                    agent_subject_email,
                    credential_mode,
                    access_role,
                    host_base_url,
                    invocation_token,
                    idempotency_key: request.idempotency_key.clone(),
                    workflow_run_id: request
                        .workflow
                        .get("runId")
                        .and_then(serde_json::Value::as_str)
                        .unwrap_or_default()
                        .to_string(),
                    workflow_trigger_id: request
                        .workflow
                        .get("trigger")
                        .and_then(serde_json::Value::as_object)
                        .and_then(|trigger| trigger.get("triggerId"))
                        .and_then(serde_json::Value::as_str)
                        .unwrap_or_default()
                        .to_string(),
                    workflow_event_spec_version: request
                        .workflow
                        .get("trigger")
                        .and_then(serde_json::Value::as_object)
                        .and_then(|trigger| trigger.get("event"))
                        .and_then(serde_json::Value::as_object)
                        .and_then(|event| event.get("specVersion"))
                        .and_then(serde_json::Value::as_str)
                        .unwrap_or_default()
                        .to_string(),
                    workflow_event_data_content_type: request
                        .workflow
                        .get("trigger")
                        .and_then(serde_json::Value::as_object)
                        .and_then(|trigger| trigger.get("event"))
                        .and_then(serde_json::Value::as_object)
                        .and_then(|event| event.get("dataContentType"))
                        .and_then(serde_json::Value::as_str)
                        .unwrap_or_default()
                        .to_string(),
                    workflow_created_by_subject_id: request
                        .workflow
                        .get("createdBy")
                        .and_then(serde_json::Value::as_object)
                        .and_then(|created_by| created_by.get("subjectId"))
                        .and_then(serde_json::Value::as_str)
                        .unwrap_or_default()
                        .to_string(),
                    tool_refs_set: request.tool_refs_set,
                    tool_ref_plugin: request
                        .tool_refs
                        .first()
                        .map(|ref_| ref_.plugin.as_str())
                        .unwrap_or_default()
                        .to_string(),
                    tool_ref_operation: request
                        .tool_refs
                        .first()
                        .map(|ref_| ref_.operation.as_str())
                        .unwrap_or_default()
                        .to_string(),
                    tool_ref_run_as: request
                        .tool_refs
                        .first()
                        .and_then(|ref_| ref_.run_as.as_ref())
                        .map(|run_as| run_as.id.as_str())
                        .unwrap_or_default()
                        .to_string(),
                    tool_ref_external_id: request
                        .tool_refs
                        .first()
                        .and_then(|ref_| ref_.run_as_external_identity.as_ref())
                        .map(|identity| identity.id.as_str())
                        .unwrap_or_default()
                        .to_string(),
                }))
            },
        )
        .expect("register operation");

    let provider = Arc::new(TestProvider::default());
    let serve_provider = Arc::clone(&provider);
    let serve_router = router.clone();
    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_provider(serve_provider, serve_router)
            .await
            .expect("serve provider");
    });

    helpers::wait_for_socket(&socket).await;

    let channel = Endpoint::try_from("http://[::]:50051")
        .expect("endpoint")
        .connect_with_connector(service_fn({
            let socket = socket.clone();
            move |_| {
                let socket = socket.clone();
                async move { UnixStream::connect(socket).await.map(TokioIo::new) }
            }
        }))
        .await
        .expect("connect channel");
    let mut client = AppProviderClient::new(channel);

    let metadata = client
        .get_metadata(())
        .await
        .expect("get metadata")
        .into_inner();
    assert!(metadata.supports_session_catalog);
    assert!(metadata.supports_post_connect);
    assert_eq!(
        metadata.min_protocol_version,
        gestalt::CURRENT_PROTOCOL_VERSION
    );
    assert_eq!(
        metadata.max_protocol_version,
        gestalt::CURRENT_PROTOCOL_VERSION
    );

    let err = client
        .start_provider(StartProviderRequest {
            name: "example".to_string(),
            config: Some(helpers::struct_from_json(
                serde_json::json!({ "greeting": "Hi" }),
            )),
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION + 1,
        })
        .await
        .expect_err("start provider should reject mismatched protocol version");
    assert_eq!(err.code(), Code::FailedPrecondition);
    assert_eq!(
        provider.greeting.lock().expect("lock greeting").as_str(),
        "",
        "provider should not be configured on protocol mismatch"
    );

    let started = client
        .start_provider(StartProviderRequest {
            name: "example".to_string(),
            config: Some(helpers::struct_from_json(
                serde_json::json!({ "greeting": "Hi" }),
            )),
            protocol_version: gestalt::CURRENT_PROTOCOL_VERSION,
        })
        .await
        .expect("start provider")
        .into_inner();
    assert_eq!(started.protocol_version, gestalt::CURRENT_PROTOCOL_VERSION);

    let response = client
        .execute(ExecuteRequest {
            operation: "greet".to_string(),
            params: Some(helpers::struct_from_json(
                serde_json::json!({ "name": "Rust" }),
            )),
            token: String::new(),
            connection_params: Default::default(),
            invocation_id: String::new(),
            invocation_token: "token-123".to_string(),
            idempotency_key: " transport-tool-123 ".to_string(),
            context: Some(RequestContext {
                subject: Some(SubjectContext {
                    id: "user:user-123".to_string(),
                    kind: "user".to_string(),
                    email: "ada@example.com".to_string(),
                    ..Default::default()
                }),
                agent_subject: Some(SubjectContext {
                    id: "user:user-456".to_string(),
                    kind: "user".to_string(),
                    email: "grace@example.com".to_string(),
                    ..Default::default()
                }),
                credential: Some(CredentialContext {
                    mode: "user".to_string(),
                    ..Default::default()
                }),
                access: Some(AccessContext {
                    policy: "sample_policy".to_string(),
                    role: "admin".to_string(),
                }),
                workflow: Some(helpers::struct_from_json(serde_json::json!({
                    "runId": "run-123",
                    "createdBy": {
                        "subjectId": "user:user-123",
                        "subjectKind": "user",
                        "displayName": "Ada",
                        "authSource": "api_token"
                    },
                    "trigger": {
                        "kind": "event",
                        "triggerId": "trigger-1",
                        "event": {
                            "id": "evt-1",
                            "source": "urn:test",
                            "specVersion": "1.0",
                            "type": "demo.refresh",
                            "dataContentType": "application/json"
                        }
                    }
                }))),
                host: Some(HostContext {
                    public_base_url: "https://gestalt.example.test".to_string(),
                }),
                tool_refs: vec![AgentToolRef {
                    app: "github".to_string(),
                    operation: "bot.getPullRequest".to_string(),
                    run_as: Some(SubjectContext {
                        id: "service_account:github-review".to_string(),
                        kind: "service_account".to_string(),
                        credential_subject_id: "service_account:github-review".to_string(),
                        display_name: "GitHub Review".to_string(),
                        auth_source: "managed_subject".to_string(),
                        email: String::new(),
                    }),
                    run_as_external_identity: Some(ExternalIdentityContext {
                        r#type: "github_identity".to_string(),
                        id: "user:12345678".to_string(),
                    }),
                    ..Default::default()
                }],
                tool_refs_set: true,
                ..Default::default()
            }),
        })
        .await
        .expect("execute")
        .into_inner();

    assert_eq!(response.status, 200);
    assert_eq!(
        response.body,
        r#"{"message":"Hi, Rust!","subject_id":"user:user-123","subject_email":"ada@example.com","agent_subject_email":"grace@example.com","credential_mode":"user","access_role":"admin","host_base_url":"https://gestalt.example.test","invocation_token":"token-123","idempotency_key":"transport-tool-123","workflow_run_id":"run-123","workflow_trigger_id":"trigger-1","workflow_event_spec_version":"1.0","workflow_event_data_content_type":"application/json","workflow_created_by_subject_id":"user:user-123","tool_refs_set":true,"tool_ref_plugin":"github","tool_ref_operation":"bot.getPullRequest","tool_ref_run_as":"service_account:github-review","tool_ref_external_id":"user:12345678"}"#
    );

    let session_catalog = client
        .get_session_catalog(GetSessionCatalogRequest {
            token: "tok".to_string(),
            connection_params: [("tenant".to_string(), "acme".to_string())]
                .into_iter()
                .collect(),
            invocation_id: String::new(),
            context: Some(RequestContext {
                subject: Some(SubjectContext {
                    id: "user:user-123".to_string(),
                    kind: "user".to_string(),
                    email: "ada@example.com".to_string(),
                    ..Default::default()
                }),
                credential: Some(CredentialContext {
                    mode: "user".to_string(),
                    ..Default::default()
                }),
                access: Some(AccessContext {
                    policy: "sample_policy".to_string(),
                    role: "viewer".to_string(),
                }),
                workflow: Some(helpers::struct_from_json(serde_json::json!({
                    "runId": "run-999",
                    "trigger": {"kind": "schedule"}
                }))),
                host: Some(HostContext {
                    public_base_url: "https://gestalt.example.test".to_string(),
                }),
                ..Default::default()
            }),
        })
        .await
        .expect("session catalog")
        .into_inner();
    let catalog = session_catalog.catalog.expect("session catalog");
    assert_eq!(catalog.name, "session-example");
    assert_eq!(
        catalog.display_name,
        "acme|user:user-123|ada@example.com|user|viewer|https://gestalt.example.test|schedule"
    );

    let resolved = client
        .resolve_http_subject(ResolveHttpSubjectRequest {
            request: Some(HttpSubjectRequest {
                binding: "command".to_string(),
                method: "POST".to_string(),
                path: "/api/v1/slack/commands/support".to_string(),
                content_type: "application/x-www-form-urlencoded".to_string(),
                headers: [(
                    "x-slack-signature".to_string(),
                    StringList {
                        values: vec!["v0=abc123".to_string()],
                    },
                )]
                .into_iter()
                .collect(),
                query: [(
                    "trace".to_string(),
                    StringList {
                        values: vec!["trace-123".to_string()],
                    },
                )]
                .into_iter()
                .collect(),
                params: Some(helpers::struct_from_json(serde_json::json!({
                    "team_id": "T123",
                    "user_id": "U456"
                }))),
                raw_body: b"team_id=T123&user_id=U456".to_vec(),
                security_scheme: "slack_signed".to_string(),
                verified_subject: "slack-app".to_string(),
                verified_claims: [("team".to_string(), "T123".to_string())]
                    .into_iter()
                    .collect(),
            }),
            context: Some(RequestContext {
                subject: Some(SubjectContext {
                    id: "user:user-123".to_string(),
                    kind: "user".to_string(),
                    email: "ada@example.com".to_string(),
                    ..Default::default()
                }),
                agent_subject: Some(SubjectContext {
                    id: "user:user-456".to_string(),
                    kind: "user".to_string(),
                    email: "grace@example.com".to_string(),
                    ..Default::default()
                }),
                external_identity: Some(ExternalIdentityContext {
                    r#type: "slack".to_string(),
                    id: "external-ada".to_string(),
                }),
                agent_external_identity: Some(ExternalIdentityContext {
                    r#type: "slack".to_string(),
                    id: "external-grace".to_string(),
                }),
                credential: Some(CredentialContext {
                    mode: "subject".to_string(),
                    ..Default::default()
                }),
                access: Some(AccessContext {
                    policy: "sample_policy".to_string(),
                    role: "admin".to_string(),
                }),
                workflow: Some(helpers::struct_from_json(serde_json::json!({
                    "runId": "run-123"
                }))),
                host: Some(HostContext {
                    public_base_url: "https://gestalt.example.test".to_string(),
                }),
                ..Default::default()
            }),
        })
        .await
        .expect("resolve http subject")
        .into_inner();
    let subject = resolved.subject.expect("resolved subject");
    assert_eq!(subject.id, "slack:T123:U456");
    assert_eq!(subject.kind, "user");
    assert_eq!(
        subject.display_name,
        "POST|/api/v1/slack/commands/support|application/x-www-form-urlencoded|v0=abc123|trace-123|team_id=T123&user_id=U456|slack_signed|slack-app|T123|ada@example.com|grace@example.com|external-ada|external-grace|subject|admin|https://gestalt.example.test"
    );
    assert_eq!(subject.auth_source, "run-123");

    let fallback = client
        .resolve_http_subject(ResolveHttpSubjectRequest {
            request: Some(HttpSubjectRequest {
                binding: "none".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("resolve http subject fallback")
        .into_inner();
    assert!(fallback.subject.is_none());
    assert_eq!(fallback.reject_status, 0);

    let rejection = client
        .resolve_http_subject(ResolveHttpSubjectRequest {
            request: Some(HttpSubjectRequest {
                binding: "reject".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("resolve http subject rejection")
        .into_inner();
    assert_eq!(rejection.reject_status, 403);
    assert_eq!(rejection.reject_message, "unmapped slack subject");

    let err = client
        .resolve_http_subject(ResolveHttpSubjectRequest {
            request: Some(HttpSubjectRequest {
                binding: "boom".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect_err("resolve http subject should return gRPC error");
    assert_eq!(err.code(), Code::Unknown);
    assert_eq!(err.message(), "resolve http subject: boom");

    let defaults = client
        .resolve_http_subject(ResolveHttpSubjectRequest {
            request: Some(HttpSubjectRequest {
                binding: "defaults".to_string(),
                ..Default::default()
            }),
            context: None,
        })
        .await
        .expect("resolve http subject with default context")
        .into_inner();
    assert_eq!(
        defaults.subject.expect("default subject").id,
        "defaults:defaults:"
    );

    let missing = client
        .resolve_http_subject(ResolveHttpSubjectRequest {
            request: None,
            context: None,
        })
        .await
        .expect("resolve http subject with missing request")
        .into_inner();
    assert!(missing.subject.is_none());

    let post_connect = client
        .post_connect(PostConnectRequest {
            token: Some(PostConnectCredential {
                id: "token-1".to_string(),
                subject_id: "user:user-123".to_string(),
                integration: "slack".to_string(),
                connection: "workspace".to_string(),
                instance: "default".to_string(),
                access_token: "access-secret".to_string(),
                refresh_token: "refresh-secret".to_string(),
                scopes: "channels:read chat:write".to_string(),
                expires_at: Some(Timestamp {
                    seconds: 100,
                    nanos: 0,
                }),
                last_refreshed_at: Some(Timestamp {
                    seconds: 200,
                    nanos: 0,
                }),
                refresh_error_count: 2,
                metadata_json: r#"{"team_id":"T123","count":3,"nested":{},"empty":""}"#.to_string(),
                created_at: Some(Timestamp {
                    seconds: 300,
                    nanos: 0,
                }),
                updated_at: Some(Timestamp {
                    seconds: 400,
                    nanos: 0,
                }),
            }),
        })
        .await
        .expect("post connect")
        .into_inner();
    let metadata = post_connect.metadata;
    assert_eq!(metadata.get("id").map(String::as_str), Some("token-1"));
    assert_eq!(
        metadata.get("subject_id").map(String::as_str),
        Some("user:user-123")
    );
    assert_eq!(
        metadata.get("integration").map(String::as_str),
        Some("slack")
    );
    assert_eq!(
        metadata.get("connection").map(String::as_str),
        Some("workspace")
    );
    assert_eq!(
        metadata.get("instance").map(String::as_str),
        Some("default")
    );
    assert_eq!(
        metadata.get("access_token_len").map(String::as_str),
        Some("13")
    );
    assert_eq!(
        metadata.get("refresh_token_present").map(String::as_str),
        Some("true")
    );
    assert_eq!(
        metadata.get("scopes").map(String::as_str),
        Some("channels:read chat:write")
    );
    assert_eq!(metadata.get("expires_at").map(String::as_str), Some("100"));
    assert_eq!(
        metadata.get("last_refreshed_at").map(String::as_str),
        Some("200")
    );
    assert_eq!(
        metadata.get("refresh_error_count").map(String::as_str),
        Some("2")
    );
    assert_eq!(
        metadata.get("metadata_json").map(String::as_str),
        Some(r#"{"team_id":"T123","count":3,"nested":{},"empty":""}"#)
    );
    assert_eq!(
        metadata.get("metadata_count").map(String::as_str),
        Some("2")
    );
    assert_eq!(metadata.get("team_id").map(String::as_str), Some("T123"));
    assert_eq!(metadata.get("has_count").map(String::as_str), Some("false"));
    assert_eq!(metadata.get("created_at").map(String::as_str), Some("300"));
    assert_eq!(metadata.get("updated_at").map(String::as_str), Some("400"));

    let invalid_metadata = client
        .post_connect(PostConnectRequest {
            token: Some(PostConnectCredential {
                metadata_json: "{not json".to_string(),
                ..Default::default()
            }),
        })
        .await
        .expect("post connect with invalid metadata json")
        .into_inner()
        .metadata;
    assert_eq!(
        invalid_metadata.get("metadata_count").map(String::as_str),
        Some("0")
    );

    let default_token = client
        .post_connect(PostConnectRequest::default())
        .await
        .expect("post connect with missing token")
        .into_inner()
        .metadata;
    assert_eq!(default_token.get("id").map(String::as_str), Some(""));
    assert_eq!(
        default_token.get("metadata_count").map(String::as_str),
        Some("0")
    );

    let err = client
        .post_connect(PostConnectRequest {
            token: Some(PostConnectCredential {
                expires_at: Some(Timestamp {
                    seconds: 0,
                    nanos: 1_000_000_000,
                }),
                ..Default::default()
            }),
        })
        .await
        .expect_err("invalid timestamp should fail");
    assert_eq!(err.code(), Code::InvalidArgument);

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn rejects_post_connect_for_unsupported_provider() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("rust-sdk-nopc.sock");
    let _socket_guard = helpers::EnvGuard::set(gestalt::ENV_PROVIDER_SOCKET, socket.as_os_str());

    let provider = Arc::new(PlainProvider);
    let serve_provider = Arc::clone(&provider);
    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_provider(serve_provider, Router::new())
            .await
            .expect("serve provider");
    });

    helpers::wait_for_socket(&socket).await;

    let channel = Endpoint::try_from("http://[::]:50051")
        .expect("endpoint")
        .connect_with_connector(service_fn({
            let socket = socket.clone();
            move |_| {
                let socket = socket.clone();
                async move { UnixStream::connect(socket).await.map(TokioIo::new) }
            }
        }))
        .await
        .expect("connect channel");
    let mut client = AppProviderClient::new(channel);

    let metadata = client
        .get_metadata(())
        .await
        .expect("get metadata")
        .into_inner();
    assert!(!metadata.supports_post_connect);

    let err = client
        .post_connect(PostConnectRequest::default())
        .await
        .expect_err("post connect should be unimplemented");
    assert_eq!(err.code(), Code::Unimplemented);

    serve_task.abort();
    let _ = serve_task.await;
}

#[tokio::test]
async fn default_http_subject_resolver_returns_empty_response() {
    let _env_lock = helpers::env_lock().lock().await;
    let socket = helpers::temp_socket("rust-http-subject.sock");
    let _socket_guard = helpers::EnvGuard::set(gestalt::ENV_PROVIDER_SOCKET, socket.as_os_str());

    let serve_task = tokio::spawn(async move {
        gestalt::runtime::serve_provider(Arc::new(PlainProvider), Router::new())
            .await
            .expect("serve provider");
    });

    helpers::wait_for_socket(&socket).await;

    let channel = Endpoint::try_from("http://[::]:50051")
        .expect("endpoint")
        .connect_with_connector(service_fn({
            let socket = socket.clone();
            move |_| {
                let socket = socket.clone();
                async move { UnixStream::connect(socket).await.map(TokioIo::new) }
            }
        }))
        .await
        .expect("connect channel");
    let mut client = AppProviderClient::new(channel);

    let response = client
        .resolve_http_subject(ResolveHttpSubjectRequest {
            request: Some(HttpSubjectRequest {
                binding: "command".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        })
        .await
        .expect("resolve http subject")
        .into_inner();
    assert!(response.subject.is_none());
    assert_eq!(response.reject_status, 0);
    assert_eq!(response.reject_message, "");

    serve_task.abort();
    let _ = serve_task.await;
}
